POLL_MS = 500
STATUS_OBSERVER_RETRY_LIMIT = 3
DIRECTION_CONFIRMATIONS = 2
TURN_PULSE_MS = 800
FIXED_TURN_PULSES = 8
FIXED_TURN_DURATION_MS = 6400
SEPARATION_DURATION_MS = 30000
SEPARATION_SAMPLES = 60
FINAL_STATUS_CONFIRMATIONS = 2

def has_flag(value, bit_value):
    return (value // bit_value) % 2 == 1

def observe_status():
    last_error = None
    for attempt_index in range(STATUS_OBSERVER_RETRY_LIMIT):
        attempt = action.try_call(id="elite-dangerous/filesystem/status", inputs={})
        if attempt["ok"]:
            status = attempt["output"]
            if status["state"] != "AVAILABLE":
                fail("FIXED_SPHERE_SEPARATION_STATUS_UNAVAILABLE: Status.json evidence is required")
            data = status["data"]
            if "Flags" not in data or "Flags2" not in data:
                fail("FIXED_SPHERE_SEPARATION_STATUS_FLAGS_MISSING: Flags and Flags2 are required")
            flags = data["Flags"]
            flags2 = data["Flags2"]
            return {
                "supercruise": has_flag(flags, 16),
                "fsdCharging": has_flag(flags, 131072),
                "overHeating": has_flag(flags, 1048576),
                "fsdHyperdriveCharging": has_flag(flags2, 524288),
                "sourceTimestamp": status["source"]["sourceTimestamp"],
            }
        last_error = attempt["error"]
        if attempt_index + 1 < STATUS_OBSERVER_RETRY_LIMIT:
            task.sleep(milliseconds=250)
    fail("FIXED_SPHERE_SEPARATION_STATUS_OBSERVER_FAILED: " + last_error)

def require_safe_supercruise(status, phase):
    if not status["supercruise"]:
        fail("FIXED_SPHERE_SEPARATION_SUPERCRUISE_REQUIRED:" + phase)
    if status["fsdCharging"] or status["fsdHyperdriveCharging"]:
        fail("FIXED_SPHERE_SEPARATION_FSD_CHARGE_ACTIVE:" + phase)
    if status["overHeating"]:
        fail("FIXED_SPHERE_SEPARATION_OVERHEATING:" + phase)

def observe_sphere():
    return action.call(id="elite-dangerous/supercruise-sphere-direction", inputs={})

def vector_control(control):
    return "_YAW_" in control

def install_failure_compensation(control=None, lease_id=None):
    action.clear_on_failure()
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    if control != None and lease_id != None:
        action.on_failure(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id}, critical=True, timeout_milliseconds=2000)

def pulse(control):
    if not vector_control(control):
        return action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": control, "holdMs": TURN_PULSE_MS})
    started = action.call(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "START", "control": control})
    lease_id = started["leaseId"]
    install_failure_compensation(control, lease_id)
    task.sleep(milliseconds=TURN_PULSE_MS)
    stopped = action.call(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id})
    install_failure_compensation()
    return stopped

def emit_update(phase, sample, observation=None, status=None, control=None, direction_confirmations=0, turn_pulses=0, separation_samples=0, final_status_confirmations=0, commanded_throttle=None, last_command=None, reason=None):
    sphere = None if observation == None else observation["sphere"]
    direction = None if observation == None else observation["direction"]
    stream.emit(type="action.fixed-supercruise-sphere-separation.update", payload={
        "phase": phase,
        "sample": sample,
        "sphereState": None if sphere == None else sphere["state"],
        "directionState": None if direction == None else direction["state"],
        "control": control,
        "directionConfirmations": direction_confirmations,
        "turnPulses": turn_pulses,
        "fixedTurnElapsedMs": turn_pulses * TURN_PULSE_MS,
        "separationSamples": separation_samples,
        "finalStatusConfirmations": final_status_confirmations,
        "supercruise": None if status == None else status["supercruise"],
        "fsdCharging": None if status == None else status["fsdCharging"],
        "fsdHyperdriveCharging": None if status == None else status["fsdHyperdriveCharging"],
        "overHeating": None if status == None else status["overHeating"],
        "statusTimestamp": None if status == None else status["sourceTimestamp"],
        "commandedThrottle": commanded_throttle,
        "lastCommand": last_command,
        "reason": reason,
    })

def main(ctx):
    install_failure_compensation()
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    sample = 1
    status = observe_status()
    require_safe_supercruise(status, "INITIAL")
    emit_update("VERIFYING_SUPERCRUISE", sample, status=status, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="SAFE_IDLE_SUPERCRUISE_CONFIRMED")

    control = None
    direction_confirmations = 0
    for _ in range(DIRECTION_CONFIRMATIONS):
        sample += 1
        observation = observe_sphere()
        sphere = observation["sphere"]
        direction = observation["direction"]
        if sphere["state"] != "DETECTED" or direction["state"] != "READY" or direction["control"] == None:
            emit_update("CONFIRMING_OUTWARD_DIRECTION", sample, observation=observation, control=control, direction_confirmations=direction_confirmations, commanded_throttle=0, reason="SPHERE_DIRECTION_NOT_READY:" + direction["reason"])
            fail("FIXED_SPHERE_SEPARATION_DIRECTION_UNKNOWN:" + direction["reason"])
        if control == None:
            control = direction["control"]
        elif direction["control"] != control:
            emit_update("CONFIRMING_OUTWARD_DIRECTION", sample, observation=observation, control=direction["control"], direction_confirmations=direction_confirmations, commanded_throttle=0, reason="DIRECTION_CONTROL_DISAGREEMENT:" + control + ":" + direction["control"])
            fail("FIXED_SPHERE_SEPARATION_DIRECTION_NOT_STABLE")
        direction_confirmations += 1
        emit_update("CONFIRMING_OUTWARD_DIRECTION", sample, observation=observation, control=control, direction_confirmations=direction_confirmations, commanded_throttle=0, reason="COMPATIBLE_DIRECTION_" + str(direction_confirmations) + "_OF_" + str(DIRECTION_CONFIRMATIONS))
        if direction_confirmations < DIRECTION_CONFIRMATIONS:
            task.sleep(milliseconds=POLL_MS)

    turn_pulses = 0
    for _ in range(FIXED_TURN_PULSES):
        pulse(control)
        turn_pulses += 1
        sample += 1
        emit_update("EXECUTING_FIXED_OUTWARD_TURN", sample, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, commanded_throttle=0, last_command=control, reason="FIXED_OUTWARD_PULSE_" + str(turn_pulses) + "_OF_" + str(FIXED_TURN_PULSES))

    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    separation_samples = 0
    for _ in range(SEPARATION_SAMPLES):
        task.sleep(milliseconds=POLL_MS)
        separation_samples += 1
        sample += 1
        status = observe_status()
        require_safe_supercruise(status, "SEPARATION_FLIGHT")
        emit_update("SEPARATION_FLIGHT", sample, status=status, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, separation_samples=separation_samples, commanded_throttle=100, last_command="SET_THROTTLE_100", reason="FIXED_SEPARATION_" + str(separation_samples) + "_OF_" + str(SEPARATION_SAMPLES))

    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    final_status_confirmations = 0
    for _ in range(FINAL_STATUS_CONFIRMATIONS):
        task.sleep(milliseconds=250)
        sample += 1
        status = observe_status()
        require_safe_supercruise(status, "FINAL_STATUS")
        final_status_confirmations += 1
        emit_update("VERIFYING_FINAL_STATUS", sample, status=status, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, separation_samples=separation_samples, final_status_confirmations=final_status_confirmations, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="FINAL_SAFE_SUPERCRUISE_" + str(final_status_confirmations) + "_OF_" + str(FINAL_STATUS_CONFIRMATIONS))

    action.clear_on_failure()
    emit_update("COMPLETED", sample, status=status, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, separation_samples=separation_samples, final_status_confirmations=final_status_confirmations, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="FIXED_TURN_AND_30_SECOND_MECHANICAL_SEPARATION_COMPLETED")
    stream.activity(message="Fixed Supercruise sphere-separation manoeuvre completed at 0% throttle", level="info")
    return {
        "schemaVersion": 1,
        "task": "FIXED_SUPERCRUISE_SPHERE_SEPARATION",
        "completed": True,
        "control": control,
        "directionConfirmations": direction_confirmations,
        "turnPulses": turn_pulses,
        "fixedTurnDurationMs": FIXED_TURN_DURATION_MS,
        "separationDurationMs": SEPARATION_DURATION_MS,
        "separationSamples": separation_samples,
        "finalStatusConfirmations": final_status_confirmations,
        "finalSupercruiseConfirmed": True,
        "finalCommandedThrottle": 0,
        "sampleCount": sample,
    }
