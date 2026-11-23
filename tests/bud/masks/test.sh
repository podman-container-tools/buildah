# !/bin/sh

function test_file {
    test -e $1 || return
    test -f $1 && cat $1 2> /dev/null
    test -d $1 && ls $1
}

# Read-only paths get mounted on /proc as filesystems
unmasked=0
for readonlypath in /proc/asound /proc/bus /proc/fs /proc/irq /proc/sys /proc/sysrq-trigger; do
    test -e $readonlypath || continue
    mount -t proc | grep -E "[[:space:]]${readonlypath}[[:space:]]" && continue
    # Path exists but doesn't show up as a mount
    unmasked=$((unmasked+1))
done

for mask in /proc/acpi /proc/kcore /proc/keys /proc/latency_stats /proc/sched_debug /proc/scsi /proc/timer_list /proc/timer_stats /sys/devices/virtual/powercap /sys/firmware /sys/fs/selinux; do
    if [ $(test_file $mask | wc -c) -gt 0 ]; then
        unmasked=$((unmasked+1))
        echo "unmasked: $mask"
    fi
done

if [ $unmasked -le 0 ]; then
    echo "all_masked"
else
    echo "some_unmasked"
fi
