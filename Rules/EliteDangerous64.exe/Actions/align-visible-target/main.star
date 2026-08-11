SAMPLE_CADENCE_MS = 750
ESCAPE_VECTOR_SAMPLE_CADENCE_MS = 350
MAX_SLEEP_STEP_MS = 250
MAX_SAMPLES = 120
MAX_COMMANDS = 80
STABLE_CONFIRMATIONS = 3
CENTER_RADIUS_PIXELS = 12.0
COARSE_DISTANCE_PIXELS = 120.0
FINE_DISTANCE_PIXELS = 40.0
COARSE_HOLD_MS = 300
MEDIUM_HOLD_MS = 160
FINE_HOLD_MS = 80
ESCAPE_VECTOR_COARSE_HOLD_MS = 500
ESCAPE_VECTOR_MEDIUM_HOLD_MS = 300
ESCAPE_VECTOR_FINE_HOLD_MS = 160
TRANSIENT_UNKNOWN_LIMIT = 8
MAX_DEADLINE_ERRORS = 5
MAX_HEAT_PERCENT = 75
MAX_UNKNOWN_HEAT_SAMPLES = 3

def emit_update(phase, target_name, sample, command_count, target=None, stable=0, command=None, hold_ms=None, reason=None, error_code=None, error=None, heat_state=None, heat_percent=None):
    stream.emit(
        type="action.align-visible-target.update",
        payload={
            "phase": phase,
            "targetName": target_name,
            "sample": sample,
            "commandCount": command_count,
            "targetState": None if target == None else target["state"],
            "offsetX": None if target == None else target["offsetX"],
            "offsetY": None if target == None else target["offsetY"],
            "centerDistancePixels": None if target == None else target["centerDistancePixels"],
            "stableConfirmations": stable,
            "command": command,
            "commandHoldMs": hold_ms,
            "heatState": heat_state,
            "heatPercent": heat_percent,
            "reason": reason,
            "observationErrorCode": error_code,
            "observationError": error,
        },
    )

def wait_for_cadence(started_ms, position_source):
    cadence_ms = ESCAPE_VECTOR_SAMPLE_CADENCE_MS if position_source == "ESCAPE_VECTOR" else SAMPLE_CADENCE_MS
    remaining = cadence_ms - (task.elapsed_milliseconds() - started_ms)
    while remaining > 0:
        step = min(remaining, MAX_SLEEP_STEP_MS)
        task.sleep(milliseconds=step)
        remaining -= step

def choose_command(target, position_source):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    distance = target["centerDistancePixels"]
    hold_ms = FINE_HOLD_MS
    if position_source == "ESCAPE_VECTOR":
        if distance > 40.0:
            hold_ms = ESCAPE_VECTOR_COARSE_HOLD_MS
        elif distance > 20.0:
            hold_ms = ESCAPE_VECTOR_MEDIUM_HOLD_MS
        else:
            hold_ms = ESCAPE_VECTOR_FINE_HOLD_MS
    elif distance > COARSE_DISTANCE_PIXELS:
        hold_ms = COARSE_HOLD_MS
    elif distance > FINE_DISTANCE_PIXELS:
        hold_ms = MEDIUM_HOLD_MS
    if abs(offset_x) >= abs(offset_y) and offset_x != 0:
        return ["YAW_RIGHT" if offset_x > 0 else "YAW_LEFT", hold_ms]
    if offset_y != 0:
        # Reference Y grows downward; Pitch Down moves a visible front target upward.
        return ["PITCH_DOWN" if offset_y > 0 else "PITCH_UP", hold_ms]
    return None

def main(ctx):
    target_name = ctx.inputs["targetName"]
    stop_before_align = ctx.inputs["stopBeforeAlign"] if "stopBeforeAlign" in ctx.inputs else True
    position_source = ctx.inputs["positionSource"] if "positionSource" in ctx.inputs else "DESTINATION"
    position_action = "elite-dangerous/escape-vector-visible-position" if position_source == "ESCAPE_VECTOR" else "elite-dangerous/supercruise-target-position"
    stable_confirmations_required = 2 if position_source == "ESCAPE_VECTOR" else STABLE_CONFIRMATIONS
    if stop_before_align:
        throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
        emit_update("STOPPING", target_name, 0, 0, command="SET_THROTTLE_0", reason=throttle["control"])

    stable = 0
    command_count = 0
    unknown_count = 0
    deadline_count = 0
    unknown_heat_count = 0
    final_target = None
    for sample in range(1, MAX_SAMPLES + 1):
        started_ms = task.elapsed_milliseconds()
        heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
        heat_state = heat["state"]
        heat_percent = heat["percent"]
        if heat_state == "KNOWN":
            unknown_heat_count = 0
            if heat_percent >= MAX_HEAT_PERCENT:
                emit_update("SAFETY_GATE", target_name, sample, command_count, stable=stable, reason="MAX_HEAT_PERCENT_REACHED", heat_state=heat_state, heat_percent=heat_percent)
                fail("visible target alignment crossed the 75 percent heat safety gate")
        else:
            unknown_heat_count += 1
            if unknown_heat_count >= MAX_UNKNOWN_HEAT_SAMPLES:
                emit_update("SAFETY_GATE", target_name, sample, command_count, stable=stable, reason="HEAT_UNKNOWN_LIMIT_REACHED", heat_state=heat_state, heat_percent=heat_percent)
                fail("visible target alignment heat remained UNKNOWN for three consecutive samples")
        position_inputs = {} if position_source == "ESCAPE_VECTOR" else {"targetName": target_name}
        attempt = action.try_call(id=position_action, inputs=position_inputs)
        if not attempt["ok"]:
            text = attempt["error"]
            bounded = text if len(text) <= 512 else text[:512]
            if attempt["errorCode"] == "JOB_DEADLINE_EXCEEDED":
                deadline_count += 1
                emit_update("OBSERVATION_ERROR", target_name, sample, command_count, stable=stable, reason="TARGET_POSITION_DEADLINE_RETRY", error_code=attempt["errorCode"], error=bounded, heat_state=heat_state, heat_percent=heat_percent)
                if deadline_count > MAX_DEADLINE_ERRORS:
                    fail("visible target deadline error limit exceeded after five skipped errors: " + text)
                wait_for_cadence(started_ms, position_source)
                continue
            emit_update("OBSERVATION_ERROR", target_name, sample, command_count, stable=stable, reason="TARGET_POSITION_OBSERVATION_FAILED", error_code=attempt["errorCode"], error=bounded, heat_state=heat_state, heat_percent=heat_percent)
            fail("visible target observation failed: " + text)

        target = attempt["output"]["target"]
        final_target = target
        if target["state"] != "DETECTED":
            unknown_count += 1
            stable = 0
            emit_update("OBSERVING", target_name, sample, command_count, target=target, stable=stable, reason="VISIBLE_TARGET_UNKNOWN", heat_state=heat_state, heat_percent=heat_percent)
            if unknown_count >= TRANSIENT_UNKNOWN_LIMIT:
                fail("visible target was UNKNOWN for eight consecutive observations")
            wait_for_cadence(started_ms, position_source)
            continue
        unknown_count = 0

        if target["centerDistancePixels"] <= CENTER_RADIUS_PIXELS:
            stable += 1
            emit_update("VERIFYING_CENTER", target_name, sample, command_count, target=target, stable=stable, reason="VISIBLE_TARGET_CENTER_CANDIDATE", heat_state=heat_state, heat_percent=heat_percent)
            if stable >= stable_confirmations_required:
                emit_update("COMPLETED", target_name, sample, command_count, target=target, stable=stable, reason="VISIBLE_TARGET_STABLY_CENTERED", heat_state=heat_state, heat_percent=heat_percent)
                stream.activity(message="Visible target precisely aligned", level="info")
                return {
                    "schemaVersion": 1,
                    "task": "ALIGN_VISIBLE_TARGET",
                    "completed": True,
                    "targetName": target_name,
                    "sampleCount": sample,
                    "commandCount": command_count,
                    "stableConfirmations": stable,
                    "finalTarget": final_target,
                }
            wait_for_cadence(started_ms, position_source)
            continue

        stable = 0
        if command_count >= MAX_COMMANDS:
            fail("visible target alignment exhausted the bounded command limit")
        pulse = choose_command(target, position_source)
        if pulse == None:
            fail("visible target remained outside center but no correction command was available")
        command = pulse[0]
        hold_ms = pulse[1]
        result = action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": command, "holdMs": hold_ms})
        command_count += 1
        stream.activity(message=command + " visible-target pulse for " + str(hold_ms) + " ms", level="info")
        emit_update("ALIGNING", target_name, sample, command_count, target=target, command=command, hold_ms=hold_ms, reason=result["control"], heat_state=heat_state, heat_percent=heat_percent)
        wait_for_cadence(started_ms, position_source)

    fail("visible target did not reach stable center before the sample limit")
