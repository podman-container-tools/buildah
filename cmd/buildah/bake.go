package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
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

// bakeVariable contains the string variable attributes currently supported
// by Buildah.
// https://docs.docker.com/build/bake/reference/#variable
type bakeVariable struct {
	Type        string  `json:"type"`
	Default     *string `json:"default"`
	Description string  `json:"description"`
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

	if len(config.Target) == 0 {
		return bakeConfig{}, fmt.Errorf("bake definition contains no targets")
	}

	return config, nil
}

// bakeVariablePattern matches ${NAME} Bake variable references.
// https://docs.docker.com/build/bake/variables/#using-variables-as-attribute-values
var bakeVariablePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// resolveBakeVariables resolves variable defaults and environment overrides,
// then interpolates variables in the supported target attributes.
// https://docs.docker.com/build/bake/variables/
// https://docs.docker.com/build/building/variables/#build-tool-configuration-variables
func resolveBakeVariables(config *bakeConfig) error {
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

		// Environment values take precedence over declared defaults.
		if !disableEnvLookup {
			if value, exists := os.LookupEnv(name); exists {
				variables[name] = value
				return value, nil
			}
		}

		if variable.Default == nil {
			return "", fmt.Errorf("unknown Bake variable %q", name)
		}

		resolving[name] = true
		defer delete(resolving, name)

		value, err := interpolateBakeStringWithResolver(*variable.Default, resolveVariable)
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

		if variable.Type != "" && variable.Type != "string" {
			return fmt.Errorf(
				"bake variable %q: type %q is not supported; only string variables are supported",
				name,
				variable.Type,
			)
		}

		if variable.Default == nil {
			if !disableEnvLookup {
				if value, exists := os.LookupEnv(name); exists {
					variables[name] = value
				}
			}
			continue
		}

		if _, err := resolveVariable(name); err != nil {
			return err
		}
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
}

func bakeInit() {
	var definition string
	var list string

	bakeCommand := &cobra.Command{
		Use:   "bake [TARGET...]",
		Short: "Build images from a Bake definition",
		Long: `Build images from a JSON Bake definition.

  Targets and groups are selected from the definition file.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return bakeCmd(cmd, args, definition, list)
		},
		Example: `buildah bake -f docker-bake.json
  buildah bake -f docker-bake.json frontend
  buildah bake -f docker-bake.json production`,
	}

	bakeCommand.SetUsageTemplate(UsageTemplate())
	bakeCommand.Flags().StringVarP(&definition, "file", "f", "docker-bake.json", "Path to the JSON Bake definition")
	bakeCommand.Flags().StringVar(&list, "list", "", "List available targets")

	rootCmd.AddCommand(bakeCommand)
}

func bakeCmd(cmd *cobra.Command, requested []string, definition, list string) error {
	config, err := readBakeConfig(definition)
	if err != nil {
		return err
	}

	if list != "" {
		if list != "targets" {
			return fmt.Errorf("unsupported Bake list type %q; only targets are supported", list)
		}

		return listBakeTargets(cmd.OutOrStdout(), config)
	}

	if err := resolveBakeVariables(&config); err != nil {
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

// buildBakeTarget translates supported Bake target attributes into Buildah
// build options.
// https://docs.docker.com/build/bake/reference/#targetcontext
// https://docs.docker.com/build/bake/reference/#targetdockerfile
// https://docs.docker.com/build/bake/reference/#targetargs
// https://docs.docker.com/build/bake/reference/#targetplatforms
func buildBakeTarget(parent *cobra.Command, target bakeTarget) error {
	contextDir := "."
	if target.Context != nil && *target.Context != "" {
		contextDir = *target.Context
	}

	// Remote contexts need separate handling because Buildah resolves the
	// Dockerfile before the remote context has been downloaded.
	if strings.Contains(contextDir, "://") || strings.HasPrefix(contextDir, "git@") {
		return fmt.Errorf("bake target %q: remote contexts are not supported yet", target.Name)
	}

	if len(target.Platforms) > 1 {
		return fmt.Errorf("bake target %q: multiple platforms are not supported yet", target.Name)
	}

	buildCommand, buildOptions, err := newBuildCommand()
	if err != nil {
		return err
	}

	if len(target.Platforms) == 1 {
		if err := buildCommand.Flags().Set("platform", target.Platforms[0]); err != nil {
			return fmt.Errorf("bake target %q: setting platform: %w", target.Name, err)
		}
	}

	// The build command needs to be a child of Bake to inherit persistent flags.
	parent.AddCommand(buildCommand)
	defer parent.RemoveCommand(buildCommand)

	dockerfile := ""
	if target.Dockerfile != nil {
		dockerfile = *target.Dockerfile
	}

	if dockerfile != "" {
		if !filepath.IsAbs(dockerfile) {
			dockerfile = filepath.Join(contextDir, dockerfile)
		}
		buildOptions.File = []string{dockerfile}
	}

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

	buildOptions.Tag = target.Tags
	if len(target.Tags) > 0 {
		buildCommand.Flags().Lookup("tag").Changed = true
	}

	if err := buildCmd(buildCommand, []string{contextDir}, buildOptions); err != nil {
		return fmt.Errorf("building Bake target %q: %w", target.Name, err)
	}

	return nil
}
