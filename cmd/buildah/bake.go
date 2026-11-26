package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// bakeConfig represents the parts of a Bake definition currently supported
// by Buildah.
// https://docs.docker.com/build/bake/reference/
type bakeConfig struct {
	Group    map[string]bakeGroup    `json:"group"`
	Target   map[string]bakeTarget   `json:"target"`
	Variable map[string]bakeVariable `json:"variable"`
}

// bakeVariable contains the variable attributes supported by the JSON
// Bake reader. Default is raw JSON because Bake variables may be strings,
// numbers, booleans, null, arrays, or objects.
// https://docs.docker.com/build/bake/reference/#variable
type bakeVariable struct {
	Type        string          `json:"type"`
	Default     json.RawMessage `json:"default"`
	Description string          `json:"description"`
}

// bakeGroup contains a list of target names.
// https://docs.docker.com/build/bake/reference/#group
type bakeGroup struct {
	Description string   `json:"description"`
	Targets     []string `json:"targets"`
}

// bakeTarget contains the target attributes currently supported by Buildah.
// https://docs.docker.com/build/bake/reference/#target
type bakeTarget struct {
	Name        string             `json:"-"`
	Description string             `json:"description"`
	Inherits    []string           `json:"inherits"`
	Context     *string            `json:"context"`
	Dockerfile  *string            `json:"dockerfile"`
	Tags        []string           `json:"tags"`
	Platforms   []string           `json:"platforms"`
	Args        map[string]*string `json:"args"`
	Target      *string            `json:"target"`
	Labels      map[string]*string `json:"labels"`
	NoCache     *bool              `json:"no-cache"`
	Pull        *bool              `json:"pull"`
	Network     *string            `json:"network"`
	Ulimits     []string           `json:"ulimits"`
}

// readBakeConfig reads a JSON Bake definition.
// https://docs.docker.com/build/bake/reference/#file-discovery
func readBakeConfig(path string) (bakeConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return bakeConfig{}, fmt.Errorf("opening Bake definition: %w", err)
	}
	defer file.Close()

	var config bakeConfig
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&config); err != nil {
		return bakeConfig{}, fmt.Errorf("parsing Bake definition: %w", err)
	}

	// Don't silently accept trailing JSON data.
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return bakeConfig{}, fmt.Errorf("parsing Bake definition: multiple JSON documents")
		}
		return bakeConfig{}, fmt.Errorf("parsing Bake definition: %w", err)
	}

	return config, nil
}

// parseBakeVariableOverrides parses command-line variable overrides and
// verifies that each variable is declared in the Bake definition.
func parseBakeVariableOverrides(config bakeConfig, values []string) (map[string]string, error) {
	overrides := make(map[string]string, len(values))
	for _, value := range values {
		name, override, ok := strings.Cut(value, "=")
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid Bake variable override %q: expected NAME=value", value)
		}
		if _, exists := config.Variable[name]; !exists {
			return nil, fmt.Errorf("unknown Bake variable %q", name)
		}
		// The last override for a variable takes precedence.
		overrides[name] = override
	}
	return overrides, nil
}

// bakeVariablePattern matches ${NAME} Bake variable references.
// https://docs.docker.com/build/bake/variables/#using-variables-as-attribute-values
var bakeVariablePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// validateBakeVariables checks that declared variable types are supported
// and that non-null defaults match their declared types.
func validateBakeVariables(config bakeConfig) error {
	variableNames := make([]string, 0, len(config.Variable))
	for name := range config.Variable {
		variableNames = append(variableNames, name)
	}
	sort.Strings(variableNames)

	for _, name := range variableNames {
		variable := config.Variable[name]

		if variable.Type != "" && variable.Type != "string" &&
			variable.Type != "bool" && variable.Type != "number" {
			return fmt.Errorf(
				"bake variable %q: type %q is not supported; only string, bool, and number variables are supported",
				name,
				variable.Type,
			)
		}

		// An omitted or null default represents an unset variable.
		if len(variable.Default) == 0 || string(variable.Default) == "null" {
			continue
		}

		// Variables without an explicit type infer their type from the default.
		if variable.Type == "" {
			continue
		}

		var defaultValue any
		decoder := json.NewDecoder(strings.NewReader(string(variable.Default)))
		decoder.UseNumber()
		if err := decoder.Decode(&defaultValue); err != nil {
			return fmt.Errorf("bake variable %q default: %w", name, err)
		}

		matches := false
		switch variable.Type {
		case "string":
			_, matches = defaultValue.(string)
		case "bool":
			_, matches = defaultValue.(bool)
		case "number":
			_, matches = defaultValue.(json.Number)
		}

		if !matches {
			return fmt.Errorf(
				"bake variable %q: default value must be a %s",
				name,
				variable.Type,
			)
		}
	}

	return nil
}

// https://docs.docker.com/build/bake/variables/
// https://docs.docker.com/build/building/variables/#build-tool-configuration-variables
// resolveBakeVariableValues resolves variable defaults and overrides without
// interpolating target attributes. The returned map is shared by normal builds
// and the --list output so both use the same precedence and default handling.
func resolveBakeVariableValues(config bakeConfig, overrides map[string]string) (map[string]string, error) {
	if err := validateBakeVariables(config); err != nil {
		return nil, err
	}

	variableNames := make([]string, 0, len(config.Variable))
	for name := range config.Variable {
		variableNames = append(variableNames, name)
	}
	sort.Strings(variableNames)

	// BUILDX_BAKE_DISABLE_VARS_ENV_LOOKUP=1 disables environment overrides.
	disableEnvLookup := os.Getenv("BUILDX_BAKE_DISABLE_VARS_ENV_LOOKUP") == "1"

	variables := make(map[string]string, len(config.Variable))
	resolving := make(map[string]bool)

	var resolveVariable func(string) (string, error)
	resolveVariable = func(name string) (string, error) {
		if value, exists := variables[name]; exists {
			return value, nil
		}

		variable, exists := config.Variable[name]
		if !exists {
			return "", fmt.Errorf("unknown Bake variable %q", name)
		}

		if resolving[name] {
			return "", fmt.Errorf("bake variable %q has a circular reference", name)
		}

		// Command-line overrides take precedence over environment values.
		if value, exists := overrides[name]; exists {
			converted, err := convertBakeVariableOverride(name, value, variable.Type, variable.Default)
			if err != nil {
				return "", err
			}
			variables[name] = converted
			return converted, nil
		}

		// Environment values take precedence over declared defaults.
		if !disableEnvLookup {
			if value, exists := os.LookupEnv(name); exists {
				converted, err := convertBakeVariableOverride(name, value, variable.Type, variable.Default)
				if err != nil {
					return "", err
				}
				variables[name] = converted
				return converted, nil
			}
		}

		if len(variable.Default) == 0 || string(variable.Default) == "null" {
			return "", fmt.Errorf("unknown Bake variable %q", name)
		}

		resolving[name] = true
		defer delete(resolving, name)

		defaultValue, err := bakeVariableDefaultString(variable.Default)
		if err != nil {
			return "", fmt.Errorf("bake variable %q default: %w", name, err)
		}
		value, err := interpolateBakeStringWithResolver(defaultValue, resolveVariable)
		if err != nil {
			return "", fmt.Errorf("bake variable %q default: %w", name, err)
		}

		variables[name] = value
		return value, nil
	}

	// Resolve in sorted order for deterministic errors. Defaults can reference
	// variables declared later in the definition.
	for _, name := range variableNames {
		variable := config.Variable[name]

		_, hasOverride := overrides[name]
		_, hasEnvironment := os.LookupEnv(name)
		hasDefault := len(variable.Default) > 0 && string(variable.Default) != "null"
		if hasOverride || (!disableEnvLookup && hasEnvironment) || hasDefault {
			if _, err := resolveVariable(name); err != nil {
				return nil, err
			}
		}
	}

	return variables, nil
}

// resolveBakeVariables resolves variables and interpolates the supported target
// attributes before target selection and inheritance.
func resolveBakeVariables(config *bakeConfig, overrides map[string]string) error {
	variables, err := resolveBakeVariableValues(*config, overrides)
	if err != nil {
		return err
	}

	targetNames := make([]string, 0, len(config.Target))
	for name := range config.Target {
		targetNames = append(targetNames, name)
	}
	sort.Strings(targetNames)

	for _, name := range targetNames {
		target := config.Target[name]

		if target.Context != nil {
			value, err := interpolateBakeString(*target.Context, variables)
			if err != nil {
				return fmt.Errorf("bake target %q context: %w", name, err)
			}
			target.Context = &value
		}

		if target.Dockerfile != nil {
			value, err := interpolateBakeString(*target.Dockerfile, variables)
			if err != nil {
				return fmt.Errorf("bake target %q dockerfile: %w", name, err)
			}
			target.Dockerfile = &value
		}

		if target.Target != nil {
			value, err := interpolateBakeString(*target.Target, variables)
			if err != nil {
				return fmt.Errorf("bake target %q target: %w", name, err)
			}
			target.Target = &value
		}

		if target.Network != nil {
			value, err := interpolateBakeString(*target.Network, variables)
			if err != nil {
				return fmt.Errorf("bake target %q network: %w", name, err)
			}
			target.Network = &value
		}

		for i, ulimit := range target.Ulimits {
			value, err := interpolateBakeString(ulimit, variables)
			if err != nil {
				return fmt.Errorf("bake target %q ulimits: %w", name, err)
			}
			target.Ulimits[i] = value
		}

		for i, tag := range target.Tags {
			value, err := interpolateBakeString(tag, variables)
			if err != nil {
				return fmt.Errorf("bake target %q tags: %w", name, err)
			}
			target.Tags[i] = value
		}

		for i, platform := range target.Platforms {
			value, err := interpolateBakeString(platform, variables)
			if err != nil {
				return fmt.Errorf("bake target %q platforms: %w", name, err)
			}
			target.Platforms[i] = value
		}

		argNames := make([]string, 0, len(target.Args))
		for argName := range target.Args {
			argNames = append(argNames, argName)
		}
		sort.Strings(argNames)

		for _, argName := range argNames {
			if target.Args[argName] == nil {
				continue
			}

			value, err := interpolateBakeString(*target.Args[argName], variables)
			if err != nil {
				return fmt.Errorf("bake target %q argument %q: %w", name, argName, err)
			}
			target.Args[argName] = &value
		}

		labelNames := make([]string, 0, len(target.Labels))
		for labelName := range target.Labels {
			labelNames = append(labelNames, labelName)
		}
		sort.Strings(labelNames)

		for _, labelName := range labelNames {
			if target.Labels[labelName] == nil {
				continue
			}

			value, err := interpolateBakeString(*target.Labels[labelName], variables)
			if err != nil {
				return fmt.Errorf("bake target %q label %q: %w", name, labelName, err)
			}
			target.Labels[labelName] = &value
		}

		config.Target[name] = target
	}

	return nil
}

// interpolateBakeString replaces ${NAME} references with resolved Bake
// variable values.
func interpolateBakeString(value string, variables map[string]string) (string, error) {
	return interpolateBakeStringWithResolver(value, func(name string) (string, error) {
		variable, exists := variables[name]
		if !exists {
			return "", fmt.Errorf("unknown Bake variable %q", name)
		}

		return variable, nil
	})
}

func interpolateBakeStringWithResolver(
	value string,
	resolve func(string) (string, error),
) (string, error) {
	var result strings.Builder

	for i := 0; i < len(value); {
		// $${NAME} leaves a literal ${NAME}.
		if strings.HasPrefix(value[i:], "$${") {
			end := strings.IndexByte(value[i+3:], '}')
			if end >= 0 {
				result.WriteString("${")
				result.WriteString(value[i+3 : i+3+end])
				result.WriteByte('}')
				i += 3 + end + 1
				continue
			}
		}

		if strings.HasPrefix(value[i:], "${") {
			match := bakeVariablePattern.FindStringSubmatchIndex(value[i:])
			if match != nil && match[0] == 0 {
				name := value[i+match[2] : i+match[3]]
				variable, err := resolve(name)
				if err != nil {
					return "", err
				}

				result.WriteString(variable)
				i += match[1]
				continue
			}
		}

		result.WriteByte(value[i])
		i++
	}

	return result.String(), nil
}

// selectBakeTargets resolves the requested target and group names.
// If nothing is specified, the default group or target is selected.
// Wildcard patterns are expanded against both target and group names.
// https://docs.docker.com/build/bake/reference/#group
// https://docs.docker.com/build/bake/reference/#target
func selectBakeTargets(config bakeConfig, requested []string) ([]bakeTarget, error) {
	if len(requested) == 0 {
		requested = []string{"default"}
	}

	var targets []bakeTarget
	seenTargets := make(map[string]bool)
	resolvingGroups := make(map[string]bool)

	for _, pattern := range requested {
		names, err := matchBakeNames(config, pattern)
		if err != nil {
			return nil, err
		}

		for _, name := range names {
			if err := addBakeTargetOrGroup(config, name, &targets, seenTargets, resolvingGroups); err != nil {
				return nil, err
			}
		}
	}

	return targets, nil
}

// matchBakeNames expands a wildcard pattern against Bake group and target
// names. Exact names are returned unchanged so that normal target resolution
// provides the existing not-found error.
func matchBakeNames(config bakeConfig, pattern string) ([]string, error) {
	if !strings.ContainsAny(pattern, "*?[") {
		return []string{pattern}, nil
	}

	matches := make(map[string]bool)

	for name := range config.Group {
		matched, err := path.Match(pattern, name)
		if err != nil {
			return nil, fmt.Errorf("invalid Bake target pattern %q: %w", pattern, err)
		}
		if matched {
			matches[name] = true
		}
	}

	for name := range config.Target {
		matched, err := path.Match(pattern, name)
		if err != nil {
			return nil, fmt.Errorf("invalid Bake target pattern %q: %w", pattern, err)
		}
		if matched {
			matches[name] = true
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no Bake target or group matching pattern %q found", pattern)
	}

	names := make([]string, 0, len(matches))
	for name := range matches {
		names = append(names, name)
	}
	sort.Strings(names)

	return names, nil
}

// addBakeTargetOrGroup recursively expands groups and adds their targets.
// Groups take precedence over targets with the same name.
func addBakeTargetOrGroup(
	config bakeConfig,
	name string,
	targets *[]bakeTarget,
	seenTargets map[string]bool,
	resolvingGroups map[string]bool,
) error {
	if group, exists := config.Group[name]; exists {
		if resolvingGroups[name] {
			return fmt.Errorf("bake group %q has a cycle", name)
		}

		resolvingGroups[name] = true
		defer delete(resolvingGroups, name)

		for _, target := range group.Targets {
			if err := addBakeTargetOrGroup(config, target, targets, seenTargets, resolvingGroups); err != nil {
				return err
			}
		}

		return nil
	}

	return addBakeTarget(config, name, targets, seenTargets)
}

func addBakeTarget(config bakeConfig, name string, targets *[]bakeTarget, seen map[string]bool) error {
	if seen[name] {
		return nil
	}

	target, err := resolveBakeTarget(config, name, make(map[string]bool))
	if err != nil {
		return err
	}

	*targets = append(*targets, target)
	seen[name] = true

	return nil
}

// resolveBakeTarget resolves a target's inherited attributes.
// https://docs.docker.com/build/bake/inheritance/
func resolveBakeTarget(config bakeConfig, name string, resolving map[string]bool) (bakeTarget, error) {
	target, exists := config.Target[name]
	if !exists {
		return bakeTarget{}, fmt.Errorf("bake target %q not found", name)
	}

	if resolving[name] {
		return bakeTarget{}, fmt.Errorf("bake target %q has an inheritance cycle", name)
	}

	resolving[name] = true
	defer delete(resolving, name)

	var resolved bakeTarget

	// Later parents take precedence over earlier parents.
	for _, parentName := range target.Inherits {
		parent, err := resolveBakeTarget(config, parentName, resolving)
		if err != nil {
			return bakeTarget{}, err
		}

		mergeBakeTarget(&resolved, parent)
	}

	mergeBakeTarget(&resolved, target)
	resolved.Name = name

	return resolved, nil
}

// mergeBakeTarget merges supported attributes from src into dst.
// https://docs.docker.com/build/bake/inheritance/
func mergeBakeTarget(dst *bakeTarget, src bakeTarget) {
	if src.Context != nil {
		dst.Context = src.Context
	}

	if src.Dockerfile != nil {
		dst.Dockerfile = src.Dockerfile
	}

	if src.Tags != nil {
		dst.Tags = append([]string{}, src.Tags...)
	}

	if src.Platforms != nil {
		dst.Platforms = append([]string{}, src.Platforms...)
	}

	if src.Target != nil {
		dst.Target = src.Target
	}

	if src.Args != nil {
		if dst.Args == nil {
			dst.Args = make(map[string]*string)
		}

		maps.Copy(dst.Args, src.Args)
	}
	if src.Labels != nil {
		if dst.Labels == nil {
			dst.Labels = make(map[string]*string)
		}

		maps.Copy(dst.Labels, src.Labels)
	}

	if src.NoCache != nil {
		dst.NoCache = src.NoCache
	}
	if src.Pull != nil {
		dst.Pull = src.Pull
	}
	if src.Network != nil {
		dst.Network = src.Network
	}
	if src.Ulimits != nil {
		dst.Ulimits = append(dst.Ulimits, src.Ulimits...)
	}
}

func bakeInit() {
	var definition string
	var list string
	var variableOverrides []string

	bakeCommand := &cobra.Command{
		Use:   "bake [TARGET...]",
		Short: "Build images from a Bake definition",
		Long: `Build images from a JSON Bake definition.

  Targets and groups are selected from the definition file.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return bakeCmd(cmd, args, definition, list, variableOverrides)
		},
		Example: `buildah bake -f docker-bake.json
  buildah bake -f docker-bake.json frontend
  buildah bake -f docker-bake.json production`,
	}

	bakeCommand.SetUsageTemplate(UsageTemplate())
	bakeCommand.Flags().StringVarP(&definition, "file", "f", "docker-bake.json", "Path to the JSON Bake definition")
	bakeCommand.Flags().StringVar(&list, "list", "", "List available targets or variables")
	bakeCommand.Flags().StringArrayVar(&variableOverrides, "var", nil, "Set a Bake variable (NAME=value)")

	rootCmd.AddCommand(bakeCommand)
}

func bakeCmd(cmd *cobra.Command, requested []string, definition, list string, variableValues []string) error {
	config, err := readBakeConfig(definition)
	if err != nil {
		return err
	}

	overrides, err := parseBakeVariableOverrides(config, variableValues)
	if err != nil {
		return err
	}

	if list != "" {
		switch list {
		case "targets":
			if err := resolveBakeVariables(&config, overrides); err != nil {
				return err
			}
			return listBakeTargets(cmd.OutOrStdout(), config)
		case "variables":
			return listBakeVariables(cmd.OutOrStdout(), config, overrides)
		default:
			return fmt.Errorf("unsupported Bake list type %q; only targets and variables are supported", list)
		}
	}

	if err := resolveBakeVariables(&config, overrides); err != nil {
		return err
	}

	targets, err := selectBakeTargets(config, requested)
	if err != nil {
		return err
	}

	for _, target := range targets {
		if err := buildBakeTarget(cmd, target); err != nil {
			return err
		}
	}

	return nil
}

// listBakeVariables prints the variables declared in the Bake file, along with
// any values supplied through the command line or environment.
func listBakeVariables(w io.Writer, config bakeConfig, overrides map[string]string) error {
	resolvedBakeVariableValues, err := resolveBakeVariableValues(config, overrides)
	if err != nil {
		return err
	}

	variableNames := make([]string, 0, len(config.Variable))
	for name := range config.Variable {
		variableNames = append(variableNames, name)
	}
	sort.Strings(variableNames)

	values := make(map[string]string, len(config.Variable))
	for _, name := range variableNames {
		value, isNull, err := bakeVariableDisplayDefault(config.Variable[name])
		if err != nil {
			return fmt.Errorf("bake variable %q: %w", name, err)
		}
		if resolved, ok := resolvedBakeVariableValues[name]; ok {
			value = resolved
			isNull = false
		}
		if isNull {
			value = "<null>"
		}
		values[name] = value
	}

	tw := tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
	if _, err := fmt.Fprintln(tw, "VARIABLE\tTYPE\tVALUE\tDESCRIPTION"); err != nil {
		return err
	}
	for _, name := range variableNames {
		variable := config.Variable[name]
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, variable.Type, values[name], variable.Description); err != nil {
			return err
		}
	}
	return tw.Flush()
}

// bakeVariableDefaultString returns a scalar default in the form used when
// substituting variables into the Bake attributes supported by Buildah.
func bakeVariableDefaultString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("has no value")
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	switch value := value.(type) {
	case string:
		return value, nil
	case json.Number:
		return value.String(), nil
	case bool:
		return strconv.FormatBool(value), nil
	case nil:
		return "", fmt.Errorf("has no value")
	default:
		return "", fmt.Errorf("complex variable defaults are not supported for build interpolation yet")
	}
}

// bakeVariableDisplayDefault formats a default for the variables listing.
// An unset variable is shown as <null>; strings are shown without JSON quotes.
func bakeVariableDisplayDefault(variable bakeVariable) (value string, isNull bool, err error) {
	if len(variable.Default) == 0 {
		return "", true, nil
	}

	var decoded any
	decoder := json.NewDecoder(strings.NewReader(string(variable.Default)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return "", false, err
	}
	if decoded == nil {
		return "", true, nil
	}

	switch decoded := decoded.(type) {
	case string:
		return decoded, false, nil
	case json.Number:
		return decoded.String(), false, nil
	case bool:
		return strconv.FormatBool(decoded), false, nil
	default:
		encoded, err := json.Marshal(decoded)
		return string(encoded), false, err
	}
}

// convertBakeVariableOverride converts an environment or command-line value
// to the primitive type declared by the variable, or inferred from its default.
func convertBakeVariableOverride(name, value, declaredType string, defaultValue json.RawMessage) (string, error) {
	typeName := declaredType
	if typeName == "" && len(defaultValue) > 0 {
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(string(defaultValue)))
		decoder.UseNumber()
		if err := decoder.Decode(&decoded); err != nil {
			return "", err
		}
		switch decoded.(type) {
		case bool:
			typeName = "bool"
		case json.Number:
			typeName = "number"
		case string, nil:
			typeName = "string"
		default:
			return "", fmt.Errorf("override for inferred complex variable %q is not supported", name)
		}
	}

	switch typeName {
	case "", "string":
		return value, nil
	case "bool":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return "", fmt.Errorf("failed to parse %s as bool: %w", name, err)
		}
		return strconv.FormatBool(parsed), nil
	case "number":
		// Use enough precision for the supplied digits instead of float64,
		// which silently rounds sufficiently large integers and decimals.
		precision := uint(len(value)*4 + 64)
		parsed, _, err := big.ParseFloat(value, 10, precision, big.ToNearestEven)
		if err != nil {
			return "", fmt.Errorf("failed to parse %s as number: %w", name, err)
		}
		if parsed.IsInf() {
			return "", fmt.Errorf("failed to parse %s as number: value is out of range", name)
		}
		return parsed.Text('g', -1), nil
	default:
		return "", fmt.Errorf("override for Bake variable %q with type %q is not supported yet", name, declaredType)
	}
}

// --list is a Docker Buildx Bake flag that lists the targets and groups
// defined in the Bake file without building them.
func listBakeTargets(w io.Writer, config bakeConfig) error {
	type entry struct {
		name        string
		description string
	}

	entries := make([]entry, 0, len(config.Target)+len(config.Group))

	for name, target := range config.Target {
		if strings.HasPrefix(name, "_") {
			continue
		}

		entries = append(entries, entry{
			name:        name,
			description: target.Description,
		})
	}

	for name, group := range config.Group {
		if strings.HasPrefix(name, "_") {
			continue
		}

		members := append([]string(nil), group.Targets...)
		sort.Strings(members)

		description := strings.Join(members, ", ")
		if group.Description != "" {
			description = group.Description
			if len(members) > 0 {
				description += " (" + strings.Join(members, ", ") + ")"
			}
		}

		entries = append(entries, entry{
			name:        name,
			description: description,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].name < entries[j].name
	})

	tw := tabwriter.NewWriter(w, 1, 8, 1, '\t', 0)
	if _, err := fmt.Fprintln(tw, "TARGET\tDESCRIPTION"); err != nil {
		return err
	}

	for _, entry := range entries {
		if _, err := fmt.Fprintf(tw, "%s\t%s\n", entry.name, entry.description); err != nil {
			return err
		}
	}

	return tw.Flush()
}

// isRemoteBakeContext reports whether context uses a remote transport
// supported by Buildah's build-context resolver. These contexts are fetched
// before Buildah resolves a relative Containerfile path.
func isRemoteBakeContext(context string) bool {
	return strings.HasPrefix(context, "http://") ||
		strings.HasPrefix(context, "https://") ||
		strings.HasPrefix(context, "git://") ||
		strings.HasPrefix(context, "github.com/")
}

// bakeDockerfilePath resolves Dockerfile paths relative to local contexts.
// For remote contexts, leave relative paths relative: Buildah resolves them
// after it has fetched the remote context.
func bakeDockerfilePath(context, dockerfile string) string {
	if filepath.IsAbs(dockerfile) || isRemoteBakeContext(context) {
		return dockerfile
	}
	return filepath.Join(context, dockerfile)
}

// buildBakeTarget translates supported Bake target attributes into Buildah
// build options.
// https://docs.docker.com/build/bake/reference/#targetcontext
// https://docs.docker.com/build/bake/reference/#targetdockerfile
// https://docs.docker.com/build/bake/reference/#targetargs
// https://docs.docker.com/build/bake/reference/#targetlabels
// https://docs.docker.com/build/bake/reference/#targetno-cache
// https://docs.docker.com/build/bake/reference/#targetplatforms
// https://docs.docker.com/build/bake/reference/#targetpull
// https://docs.docker.com/build/bake/reference/#targetnetwork
// https://docs.docker.com/build/bake/reference/#targetulimits
func buildBakeTarget(parent *cobra.Command, target bakeTarget) error {
	contextDir := "."
	if target.Context != nil && *target.Context != "" {
		contextDir = *target.Context
	}

	multiPlatform := len(target.Platforms) > 1
	if multiPlatform && len(target.Tags) == 0 {
		return fmt.Errorf("bake target %q: at least one tag is required when building multiple platforms", target.Name)
	}

	buildCommand, buildOptions, err := newBuildCommand()
	if err != nil {
		return err
	}

	for _, platform := range target.Platforms {
		if err := buildCommand.Flags().Set("platform", platform); err != nil {
			return fmt.Errorf("bake target %q: setting platform: %w", target.Name, err)
		}
	}

	if multiPlatform {
		// Buildah creates a manifest list instead of tagging individual images
		// when building for multiple platforms.
		if err := buildCommand.Flags().Set("manifest", target.Tags[0]); err != nil {
			return fmt.Errorf("bake target %q: setting manifest: %w", target.Name, err)
		}
	}

	if target.NoCache != nil {
		if err := buildCommand.Flags().Set("no-cache", fmt.Sprintf("%t", *target.NoCache)); err != nil {
			return fmt.Errorf("bake target %q: setting no-cache: %w", target.Name, err)
		}
	}

	if target.Pull != nil {
		pullPolicy := "missing"
		if *target.Pull {
			pullPolicy = "always"
		}

		if err := buildCommand.Flags().Set("pull", pullPolicy); err != nil {
			return fmt.Errorf("bake target %q: setting pull: %w", target.Name, err)
		}
	}

	// Docker Bake's "default" network uses the builder's default networking.
	// Leave Buildah's --network flag unset in that case.
	if target.Network != nil && *target.Network != "" && *target.Network != "default" {
		if err := buildCommand.Flags().Set("network", *target.Network); err != nil {
			return fmt.Errorf("bake target %q: setting network: %w", target.Name, err)
		}
	}

	// Set ulimits through the flag so that CommonBuildOptions picks them up.
	for _, ulimit := range target.Ulimits {
		if err := buildCommand.Flags().Set("ulimit", ulimit); err != nil {
			return fmt.Errorf("bake target %q: setting ulimit: %w", target.Name, err)
		}
	}

	// The build command needs to be a child of Bake to inherit persistent flags.
	parent.AddCommand(buildCommand)
	defer parent.RemoveCommand(buildCommand)

	// Buildx Bake resolves target.dockerfile to "Dockerfile" when it is
	// omitted (see https://docs.docker.com/build/bake/reference/#targetdockerfile).
	// Keep that Bake behaviour here rather than using Buildah's normal
	// Containerfile-first discovery (see containers/common's Containerfile.5:
	// https://github.com/containers/common/blob/main/docs/Containerfile.5.md).
	dockerfile := "Dockerfile"
	if target.Dockerfile != nil && *target.Dockerfile != "" {
		dockerfile = *target.Dockerfile
	}

	buildOptions.File = []string{bakeDockerfilePath(contextDir, dockerfile)}

	if target.Target != nil && *target.Target != "" {
		buildOptions.Target = *target.Target
	}

	if len(target.Args) > 0 {
		argNames := make([]string, 0, len(target.Args))
		for name, value := range target.Args {
			if value != nil {
				argNames = append(argNames, name)
			}
		}
		sort.Strings(argNames)

		for _, name := range argNames {
			buildOptions.BuildArg = append(
				buildOptions.BuildArg,
				fmt.Sprintf("%s=%s", name, *target.Args[name]),
			)
		}

		if len(argNames) > 0 {
			buildCommand.Flags().Lookup("build-arg").Changed = true
		}
	}

	if !multiPlatform {
		buildOptions.Tag = target.Tags
		if len(target.Tags) > 0 {
			buildCommand.Flags().Lookup("tag").Changed = true
		}
	}

	if len(target.Labels) > 0 {
		labelNames := make([]string, 0, len(target.Labels))
		for name, value := range target.Labels {
			if value != nil {
				labelNames = append(labelNames, name)
			}
		}
		sort.Strings(labelNames)

		for _, name := range labelNames {
			buildOptions.Label = append(
				buildOptions.Label,
				fmt.Sprintf("%s=%s", name, *target.Labels[name]),
			)
		}

		if len(labelNames) > 0 {
			buildCommand.Flags().Lookup("label").Changed = true
		}
	}

	if err := buildCmd(buildCommand, []string{contextDir}, buildOptions); err != nil {
		return fmt.Errorf("building Bake target %q: %w", target.Name, err)
	}

	// Buildah's --manifest option accepts one name. Add the remaining Bake
	// tags as aliases of the manifest list after the build has completed.
	if multiPlatform && len(target.Tags) > 1 {
		if err := tagCmd(parent, target.Tags); err != nil {
			return fmt.Errorf("tagging Bake target %q manifest: %w", target.Name, err)
		}
	}

	return nil
}
