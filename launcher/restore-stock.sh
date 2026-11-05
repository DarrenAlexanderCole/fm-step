#!/bin/sh
# FM-STEP last-resort stock restore. Runs as ExecStopPost of the supervisor's
# transient unit, i.e. after the supervisor exits, crashes or is killed by the
# systemd watchdog, and after systemd has killed every process in that unit
# (KillMode=control-group), including the FM-STEP application.
# It is deliberately tiny and independent of the supervisor binary.
# Runtime-only: no enable/disable/mask, no writes outside /run and the SD log.

RUN="${FMSTEP_TEST_ROOT:-}/run/fmstep"
UNIT=ac50.service
SD=$(cat "$RUN/sd_root" 2>/dev/null)
LOG="$RUN/restore-stock.log"

log() {
    line="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) up=$(cut -d ' ' -f 1 /proc/uptime 2>/dev/null) RESTORE_SH $*"
    echo "$line" >> "$LOG" 2>/dev/null
    [ -n "$SD" ] && [ -d "$SD/FMSTEP/logs" ] && echo "$line" >> "$SD/FMSTEP/logs/recovery.log" 2>/dev/null
}

state=$(systemctl is-system-running 2>/dev/null)
if [ "$state" = stopping ]; then
    log "SYSTEM_STOPPING skip"
    exit 0
fi

log "INVOKED service_result=${SERVICE_RESULT:-?} exit_code=${EXIT_CODE:-?} exit_status=${EXIT_STATUS:-?}"

if systemctl is-active --quiet "$UNIT"; then
    log "STOCK_ALREADY_ACTIVE"
    exit 0
fi

i=0
while [ $i -lt 3 ]; do
    i=$((i + 1))
    systemctl reset-failed "$UNIT" 2>/dev/null
    systemctl start "$UNIT" 2>>"$LOG"
    n=0
    while [ $n -lt 20 ]; do
        sleep 1
        n=$((n + 1))
        if systemctl is-active --quiet "$UNIT"; then
            log "STOCK_STARTED attempt=$i"
            exit 0
        fi
    done
    log "STOCK_START_FAILED attempt=$i"
done

log "STOCK_RESTORE_GAVE_UP rebooting"
sync
systemctl --no-block reboot
exit 1
