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
SEARCH_UNKNOWN_LIMIT = 12
SEARCH_PULSE_MS = 600
SEARCH_LEFT_SAMPLES = 4
SEARCH_HEAT_RETRY_SETTLE_MS = 500
MAX_DEADLINE_ERRORS = 5
MAX_HEAT_PERCENT = 75
HIGH_HEAT_CONFIRMATIONS = 2
MAX_UNKNOWN_HEAT_SAMPLES = 3
ESCAPE_CHARGE_LAST_KNOWN_MAX_PERCENT = 60
ESCAPE_CHARGE_UNKNOWN_GRACE_MS = 4000

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
    search_when_unknown = ctx.inputs["searchWhenUnknown"] if "searchWhenUnknown" in ctx.inputs else False
    heat_policy = ctx.inputs["heatPolicy"] if "heatPolicy" in ctx.inputs else "STRICT"
    if heat_policy == "ESCAPE_VECTOR_CHARGE" and position_source != "ESCAPE_VECTOR":
        fail("ESCAPE_VECTOR_CHARGE heat policy requires the Escape Vector position source")
    stable_confirmations_required = 2 if position_source == "ESCAPE_VECTOR" else STABLE_CONFIRMATIONS
    if stop_before_align:
        throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
        emit_update("STOPPING", target_name, 0, 0, command="SET_THROTTLE_0", reason=throttle["control"])

    stable = 0
    command_count = 0
    unknown_count = 0
    deadline_count = 0
    unknown_heat_count = 0
    high_heat_count = 0
    last_known_heat_percent = None
    last_known_heat_ms = None
    final_target = None
    for sample in range(1, MAX_SAMPLES + 1):
        started_ms = task.elapsed_milliseconds()
        heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
        heat_state = heat["state"]
        heat_percent = heat["percent"]
        if heat_state != "KNOWN" and search_when_unknown:
            # Search pulses move the cockpit HUD enough to push the heat digits
            # outside their inertia-tolerant ROI. Let flight assist damp the
            # view and retry the same safety observation; do not weaken the
            # three-UNKNOWN failure contract.
            remaining_settle_ms = SEARCH_HEAT_RETRY_SETTLE_MS
            while remaining_settle_ms > 0:
                settle_step_ms = min(remaining_settle_ms, MAX_SLEEP_STEP_MS)
                task.sleep(milliseconds=settle_step_ms)
                remaining_settle_ms -= settle_step_ms
            heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
            heat_state = heat["state"]
            heat_percent = heat["percent"]
        if heat_state == "KNOWN":
            unknown_heat_count = 0
            if heat_percent >= MAX_HEAT_PERCENT:
                high_heat_count += 1
                if high_heat_count >= HIGH_HEAT_CONFIRMATIONS:
                    emit_update("SAFETY_GATE", target_name, sample, command_count, stable=stable, reason="MAX_HEAT_PERCENT_CONFIRMED", heat_state=heat_state, heat_percent=heat_percent)
                    fail("visible target alignment crossed the confirmed 75 percent heat safety gate")
                # A single constrained OCR frame can concatenate the real heat
                # digits with a neighbouring HUD digit (23 -> 238). Stop
                # steering for this cadence and require an independent high
                # sample before treating it as real heat.
                emit_update("SAFETY_GATE", target_name, sample, command_count, stable=stable, reason="HIGH_HEAT_AWAITING_CONFIRMATION", heat_state=heat_state, heat_percent=heat_percent)
                wait_for_cadence(started_ms, position_source)
                continue
            high_heat_count = 0
            last_known_heat_percent = heat_percent
            last_known_heat_ms = task.elapsed_milliseconds()
        else:
            high_heat_count = 0
            unknown_heat_count += 1
            charge_grace = (
                heat_policy == "ESCAPE_VECTOR_CHARGE" and
                last_known_heat_percent != None and
                last_known_heat_percent <= ESCAPE_CHARGE_LAST_KNOWN_MAX_PERCENT and
                task.elapsed_milliseconds() - last_known_heat_ms <= ESCAPE_CHARGE_UNKNOWN_GRACE_MS
            )
            if charge_grace:
                emit_update("SAFETY_GATE", target_name, sample, command_count, stable=stable, reason="HEAT_UNKNOWN_ESCAPE_CHARGE_GRACE:" + str(last_known_heat_percent), heat_state=heat_state, heat_percent=heat_percent)
            elif unknown_heat_count >= MAX_UNKNOWN_HEAT_SAMPLES:
                emit_update("SAFETY_GATE", target_name, sample, command_count, stable=stable, reason="HEAT_UNKNOWN_LIMIT_REACHED", heat_state=heat_state, heat_percent=heat_percent)
                fail("visible target alignment heat remained UNKNOWN for three consecutive samples")
        if position_source == "ESCAPE_VECTOR":
            attempt = action.try_call(id="elite-dangerous/escape-vector-visible-position", inputs={})
        else:
            attempt = action.try_call(id="elite-dangerous/supercruise-target-position", inputs={"targetName": target_name})
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
            if search_when_unknown and position_source == "DESTINATION" and unknown_count <= SEARCH_UNKNOWN_LIMIT:
                if command_count >= MAX_COMMANDS:
                    fail("visible target search exhausted the bounded command limit")
                search_control = "YAW_LEFT" if unknown_count <= SEARCH_LEFT_SAMPLES else "YAW_RIGHT"
                search_result = action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": search_control, "holdMs": SEARCH_PULSE_MS})
                command_count += 1
                stream.activity(message=search_control + " bounded visible-target search pulse", level="info")
                emit_update("SEARCHING", target_name, sample, command_count, target=target, stable=stable, command=search_control, hold_ms=SEARCH_PULSE_MS, reason="REQUESTED_TARGET_OUTSIDE_OCR_ROI:" + search_result["control"], heat_state=heat_state, heat_percent=heat_percent)
                wait_for_cadence(started_ms, position_source)
                continue
            emit_update("OBSERVING", target_name, sample, command_count, target=target, stable=stable, reason="VISIBLE_TARGET_UNKNOWN", heat_state=heat_state, heat_percent=heat_percent)
            if unknown_count >= (SEARCH_UNKNOWN_LIMIT if search_when_unknown else TRANSIENT_UNKNOWN_LIMIT):
                fail("visible target remained UNKNOWN after its bounded observation or search window")
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
