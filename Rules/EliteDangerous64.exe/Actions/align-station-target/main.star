SAMPLE_CADENCE_MS = 1000
MAX_SLEEP_STEP_MS = 250
MAX_COMMANDS = 120
MAX_SAMPLES = 240
STABLE_CENTER_CONFIRMATIONS = 3
SUSTAINED_DISTANCE_PIXELS = 40
FINE_DISTANCE_PIXELS = 16
DIAGONAL_COMPONENT_MIN_PIXELS = 8
COARSE_HOLD_MS = 800
MEDIUM_HOLD_MS = 300
FINE_HOLD_MS = 250
RECOVERY_HOLD_MS = 400
MIN_OBSERVED_MOVEMENT_PIXELS = 1
NO_MOVEMENT_LIMIT = 4
AWAY_TREND_LIMIT = 5
AMBIGUOUS_PRESENTATION_LIMIT = 5
TRANSIENT_MISSING_LIMIT = 3

def empty_target():
    return {
        "detected": None,
        "presentation": None,
        "hemisphere": None,
        "offsetX": None,
        "offsetY": None,
        "centerDistancePixels": None,
        "centerZone": {"inside": None},
    }

def emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="NONE", command=None, command_result=None, reason=None, information=None, command_hold_ms=None, observed_movement_pixels=None, no_movement_count=0, distance_delta_pixels=None, away_trend_count=0, lease_id=None, lease_state=None, sample_started_ms=None, sample_duration_ms=None, sample_interval_ms=None):
    stream.emit(
        type="action.align-station-target.update",
        payload={
            "phase": phase,
            "sample": sample,
            "commandCount": command_count,
            "controlMode": control_mode,
            "targetDetected": target["detected"],
            "targetPresentation": target["presentation"],
            "targetHemisphere": target["hemisphere"],
            "offsetX": target["offsetX"],
            "offsetY": target["offsetY"],
            "centerDistancePixels": target["centerDistancePixels"],
            "centerZoneInside": target["centerZone"]["inside"],
            "stableConfirmations": stable_confirmations,
            "command": command,
            "commandHoldMs": command_hold_ms,
            "commandResult": command_result,
            "leaseId": lease_id,
            "leaseState": lease_state,
            "sampleStartedMs": sample_started_ms,
            "sampleDurationMs": sample_duration_ms,
            "sampleIntervalMs": sample_interval_ms,
            "observedMovementPixels": observed_movement_pixels,
            "noMovementCount": no_movement_count,
            "distanceDeltaPixels": distance_delta_pixels,
            "awayTrendCount": away_trend_count,
            "reason": reason,
            "information": information,
        },
    )

def choose_front_control(target):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    if abs(offset_x) >= abs(offset_y) and offset_x != 0:
        return "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
    if offset_y != 0:
        # Screen Y grows downward. Pitch up moves the front marker downward.
        return "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
    return None

def choose_pulse(target, no_movement_count):
    control = choose_front_control(target)
    if control == None:
        return None
    distance = target["centerDistancePixels"]
    hold_ms = COARSE_HOLD_MS
    if distance <= FINE_DISTANCE_PIXELS:
        hold_ms = FINE_HOLD_MS
    elif distance <= SUSTAINED_DISTANCE_PIXELS:
        hold_ms = MEDIUM_HOLD_MS
    if no_movement_count >= 2:
        hold_ms = RECOVERY_HOLD_MS
    return [control, hold_ms]

def choose_sustained_control(target):
    if target["presentation"] == "HOLLOW":
        # The rear projection crosses the antipode. Keep one direction until
        # the topology becomes solid rather than reversing from offset signs.
        return "YAW_LEFT"
    if target["centerDistancePixels"] > SUSTAINED_DISTANCE_PIXELS:
        offset_x = target["offsetX"]
        offset_y = target["offsetY"]
        if abs(offset_x) >= DIAGONAL_COMPONENT_MIN_PIXELS and abs(offset_y) >= DIAGONAL_COMPONENT_MIN_PIXELS:
            pitch = "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
            yaw = "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
            return pitch + "_" + yaw
        return choose_front_control(target)
    return None

def is_vector_control(control):
    return "_YAW_" in control

def opposite_control(control):
    opposites = {
        "YAW_LEFT": "YAW_RIGHT",
        "YAW_RIGHT": "YAW_LEFT",
        "PITCH_UP": "PITCH_DOWN",
        "PITCH_DOWN": "PITCH_UP",
        "PITCH_UP_YAW_LEFT": "PITCH_DOWN_YAW_RIGHT",
        "PITCH_UP_YAW_RIGHT": "PITCH_DOWN_YAW_LEFT",
        "PITCH_DOWN_YAW_LEFT": "PITCH_UP_YAW_RIGHT",
        "PITCH_DOWN_YAW_RIGHT": "PITCH_UP_YAW_LEFT",
    }
    return opposites[control] if control in opposites else None

def start_hold(control):
    if is_vector_control(control):
        return action.call(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "START", "control": control})
    return action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "START", "control": control})

def renew_hold(control, lease_id):
    if is_vector_control(control):
        return action.call(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "RENEW", "control": control, "leaseId": lease_id})
    return action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "RENEW", "control": control, "leaseId": lease_id})

def stop_hold(control, lease_id):
    if is_vector_control(control):
        return action.call(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id})
    return action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id})

def register_hold_failure(control, lease_id):
    if is_vector_control(control):
        action.on_failure(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id})
    else:
        action.on_failure(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id})

def wait_for_sample_cadence(sample_started_ms):
    remaining = SAMPLE_CADENCE_MS - (task.elapsed_milliseconds() - sample_started_ms)
    while remaining > 0:
        step = remaining
        if step > MAX_SLEEP_STEP_MS:
            step = MAX_SLEEP_STEP_MS
        task.sleep(milliseconds=step)
        remaining -= step

def release_lease(lease_id, control, phase, sample, command_count, target, stable_confirmations, reason, sample_started_ms, sample_duration_ms, sample_interval_ms):
    if lease_id == None:
        return
    release_result = stop_hold(control, lease_id)
    action.clear_on_failure()
    emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="SUSTAINED", command=control, command_result=release_result, lease_id=lease_id, lease_state="RELEASED", sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=reason)

def main(ctx):
    mode = ctx.inputs["mode"] if "mode" in ctx.inputs else "ALIGN"
    tracking_samples = int(ctx.inputs["trackingSamples"]) if "trackingSamples" in ctx.inputs else 120
    stop_before_align = ctx.inputs["stopBeforeAlign"] if "stopBeforeAlign" in ctx.inputs else True
    sample_limit = tracking_samples if mode == "TRACK" else MAX_SAMPLES

    if stop_before_align:
        stream.activity(message="Stopping ship before compass alignment", level="info")
        throttle_result = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
        emit_update("STOPPING", 0, 0, empty_target(), 0, command="SET_THROTTLE_0", command_result=throttle_result)

    sample = 0
    command_count = 0
    stable_confirmations = 0
    center_contact_count = 0
    max_consecutive_center = 0
    previous_phase = None
    final_observation = None
    commanded_target = None
    commanded_control = None
    no_movement_count = 0
    pitch_no_movement_count = 0
    away_trend_count = 0
    active_lease_id = None
    active_lease_control = None
    previous_sample_started_ms = None
    ambiguous_presentation_count = 0
    transient_missing_count = 0
    target_seen = False

    for _ in range(sample_limit):
        sample_started_ms = task.elapsed_milliseconds()
        attempt = action.try_call(id="elite-dangerous/compass", inputs={})
        sample_completed_ms = task.elapsed_milliseconds()
        sample_duration_ms = sample_completed_ms - sample_started_ms
        sample_interval_ms = None if previous_sample_started_ms == None else sample_started_ms - previous_sample_started_ms
        previous_sample_started_ms = sample_started_ms
        sample += 1
        if not attempt["ok"]:
            release_lease(active_lease_id, active_lease_control, "OBSERVATION_ERROR", sample, command_count, empty_target(), stable_confirmations, "SUSTAINED_CONTROL_RELEASED_BEFORE_FAILURE", sample_started_ms, sample_duration_ms, sample_interval_ms)
            emit_update("OBSERVATION_ERROR", sample, command_count, empty_target(), stable_confirmations, lease_id=active_lease_id, lease_state="RELEASED" if active_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=attempt["error"])
            fail("Compass observation failed: " + attempt["error"])

        observation = attempt["output"]
        target = observation["target"]
        final_observation = observation
        observed_movement = None
        distance_delta = None
        if mode == "ALIGN" and commanded_target != None and target["detected"]:
            observed_movement = max(
                abs(target["offsetX"] - commanded_target["offsetX"]),
                abs(target["offsetY"] - commanded_target["offsetY"]),
            )
            if observed_movement < MIN_OBSERVED_MOVEMENT_PIXELS:
                no_movement_count += 1
                if commanded_control in ["PITCH_UP", "PITCH_DOWN"]:
                    pitch_no_movement_count += 1
                else:
                    pitch_no_movement_count = 0
            else:
                no_movement_count = 0
                pitch_no_movement_count = 0
            distance_delta = target["centerDistancePixels"] - commanded_target["centerDistancePixels"]
            if commanded_target["presentation"] == "SOLID" and target["presentation"] == "SOLID":
                if distance_delta >= 1:
                    away_trend_count += 1
                else:
                    away_trend_count = 0
            else:
                away_trend_count = 0

        if not target["detected"]:
            release_lease(active_lease_id, active_lease_control, "OBSERVING", sample, command_count, target, 0, "SUSTAINED_CONTROL_RELEASED_BEFORE_FAILURE", sample_started_ms, sample_duration_ms, sample_interval_ms)
            released_lease_id = active_lease_id
            active_lease_id = None
            active_lease_control = None
            commanded_target = None
            commanded_control = None
            transient_missing_count += 1
            reason = "TARGET_NOT_DETECTED" if not target_seen else "TARGET_NOT_DETECTED_TRANSIENT"
            emit_update("OBSERVING", sample, command_count, target, 0, lease_id=released_lease_id, lease_state="RELEASED" if released_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=reason)
            if not target_seen or transient_missing_count >= TRANSIENT_MISSING_LIMIT:
                fail("Compass target is not detected; establish the intended Station target lock first")
            wait_for_sample_cadence(sample_started_ms)
            continue
        target_seen = True
        transient_missing_count = 0
        if target["presentation"] == "UNKNOWN":
            ambiguous_presentation_count += 1
            released_control = active_lease_control
            release_lease(active_lease_id, active_lease_control, "OBSERVING", sample, command_count, target, 0, "SUSTAINED_CONTROL_RELEASED_FOR_AMBIGUOUS_OBSERVATION", sample_started_ms, sample_duration_ms, sample_interval_ms)
            released_lease_id = active_lease_id
            active_lease_id = None
            active_lease_control = None
            commanded_target = None
            commanded_control = None
            emit_update("OBSERVING", sample, command_count, target, 0, lease_id=released_lease_id, lease_state="RELEASED" if released_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="TARGET_PRESENTATION_UNKNOWN")
            brake_control = opposite_control(released_control) if mode == "ALIGN" and released_control != None else None
            if brake_control != None:
                if command_count >= MAX_COMMANDS:
                    emit_update("OBSERVING", sample, command_count, target, 0, reason="COMMAND_LIMIT_REACHED")
                    fail("Compass alignment exhausted the bounded command limit")
                brake_result = action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": brake_control, "holdMs": FINE_HOLD_MS})
                command_count += 1
                stream.activity(message=brake_control + " transition brake for " + str(FINE_HOLD_MS) + " ms", level="info")
                emit_update("OBSERVING", sample, command_count, target, 0, control_mode="PULSE", command=brake_control, command_result=brake_result, command_hold_ms=FINE_HOLD_MS, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="AMBIGUOUS_TRANSITION_BRAKE")
            if ambiguous_presentation_count >= AMBIGUOUS_PRESENTATION_LIMIT:
                fail("Compass target hollow or solid presentation remained ambiguous")
            wait_for_sample_cadence(sample_started_ms)
            continue
        ambiguous_presentation_count = 0
        if mode == "ALIGN" and no_movement_count >= NO_MOVEMENT_LIMIT:
            if pitch_no_movement_count >= NO_MOVEMENT_LIMIT:
                information = {
                    "code": "ED_PITCH_INPUT_CONTEXT_NOT_READY",
                    "message": "Elite Dangerous accepted binding-resolved Pitch injections but produced no Compass movement. This matches the reproduced ED startup state where Pitch remains inactive until the configured controller is powered on or reconnected.",
                    "recommendedAction": "Power on or reconnect the configured controller, then retry without restarting Elite Dangerous. Do not use XInput enumeration as the Gate.",
                }
                release_lease(active_lease_id, active_lease_control, "OBSERVING", sample, command_count, target, 0, "SUSTAINED_CONTROL_RELEASED_BEFORE_FAILURE", sample_started_ms, sample_duration_ms, sample_interval_ms)
                emit_update("OBSERVING", sample, command_count, target, 0, lease_id=active_lease_id, lease_state="RELEASED" if active_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="ED_PITCH_INPUT_CONTEXT_NOT_READY", information=information, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
                fail("ED_PITCH_INPUT_CONTEXT_NOT_READY: repeated Pitch input produced no Compass movement; power on or reconnect the configured controller, then retry without restarting Elite Dangerous")
            release_lease(active_lease_id, active_lease_control, "OBSERVING", sample, command_count, target, 0, "SUSTAINED_CONTROL_RELEASED_BEFORE_FAILURE", sample_started_ms, sample_duration_ms, sample_interval_ms)
            emit_update("OBSERVING", sample, command_count, target, 0, lease_id=active_lease_id, lease_state="RELEASED" if active_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="ATTITUDE_CONTROL_NO_PROGRESS", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            fail("Ship attitude control produced no measurable Compass movement")
        if away_trend_count >= AWAY_TREND_LIMIT and mode == "ALIGN":
            release_lease(active_lease_id, active_lease_control, "OBSERVING", sample, command_count, target, 0, "SUSTAINED_CONTROL_RELEASED_BEFORE_FAILURE", sample_started_ms, sample_duration_ms, sample_interval_ms)
            emit_update("OBSERVING", sample, command_count, target, 0, lease_id=active_lease_id, lease_state="RELEASED" if active_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="ATTITUDE_CONTROL_MOVING_AWAY", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            fail("Ship attitude control moved the front Compass target away from center")

        phase = "TURNING_TO_FRONT"
        if target["presentation"] == "SOLID":
            if target["centerZone"]["inside"]:
                phase = "VERIFYING_CENTER"
            elif target["centerDistancePixels"] > SUSTAINED_DISTANCE_PIXELS:
                phase = "COARSE_ALIGN"
            else:
                phase = "FINE_ALIGN"

        desired_sustained = choose_sustained_control(target) if mode == "ALIGN" else None
        if active_lease_id != None and desired_sustained != active_lease_control:
            released_control = active_lease_control
            released_id = active_lease_id
            release_result = stop_hold(released_control, released_id)
            action.clear_on_failure()
            active_lease_id = None
            active_lease_control = None
            commanded_target = None
            commanded_control = None
            emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="SUSTAINED", command=released_control, command_result=release_result, lease_id=released_id, lease_state="RELEASED", sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="SUSTAINED_CONTROL_RELEASED", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)

        if phase != previous_phase:
            if phase == "TURNING_TO_FRONT":
                stream.activity(message="Holding coarse turn until target reaches front hemisphere", level="info")
            elif phase == "COARSE_ALIGN":
                stream.activity(message="Holding coarse compass alignment", level="info")
            elif phase == "FINE_ALIGN":
                stream.activity(message="Fine pulse compass alignment", level="info")
            elif phase == "VERIFYING_CENTER":
                stream.activity(message="Verifying stable compass center", level="info")
            previous_phase = phase

        if target["presentation"] == "SOLID" and target["centerZone"]["inside"]:
            stable_confirmations += 1
            center_contact_count += 1
            if stable_confirmations > max_consecutive_center:
                max_consecutive_center = stable_confirmations
        else:
            stable_confirmations = 0

        if mode == "ALIGN" and stable_confirmations >= STABLE_CENTER_CONFIRMATIONS:
            emit_update("COMPLETED", sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="SOLID_TARGET_STABLY_CENTERED")
            stream.activity(message="Station target aligned", level="info")
            return {
                "schemaVersion": 1,
                "task": "ALIGN_STATION_TARGET",
                "mode": mode,
                "completed": True,
                "finalPhase": "COMPLETED",
                "sampleCount": sample,
                "commandCount": command_count,
                "stableConfirmations": stable_confirmations,
                "centerContactCount": center_contact_count,
                "maxConsecutiveCenter": max_consecutive_center,
                "finalObservation": final_observation,
            }

        if mode == "TRACK" and sample >= tracking_samples:
            emit_update("TRACKING_WINDOW_COMPLETED", sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="BOUNDED_TRACKING_WINDOW_COMPLETED", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            stream.activity(message="Moving-target tracking window completed", level="info")
            return {
                "schemaVersion": 1,
                "task": "ALIGN_STATION_TARGET",
                "mode": mode,
                "completed": True,
                "finalPhase": "TRACKING_WINDOW_COMPLETED",
                "sampleCount": sample,
                "commandCount": command_count,
                "stableConfirmations": stable_confirmations,
                "centerContactCount": center_contact_count,
                "maxConsecutiveCenter": max_consecutive_center,
                "finalObservation": final_observation,
            }

        if desired_sustained != None:
            hold_result = None
            reason = "SUSTAINED_CONTROL_RENEWED"
            if active_lease_id == None:
                if command_count >= MAX_COMMANDS:
                    emit_update(phase, sample, command_count, target, stable_confirmations, reason="COMMAND_LIMIT_REACHED")
                    fail("Compass alignment exhausted the bounded command limit")
                hold_result = start_hold(desired_sustained)
                active_lease_id = hold_result["leaseId"]
                active_lease_control = desired_sustained
                register_hold_failure(active_lease_control, active_lease_id)
                command_count += 1
                reason = "SUSTAINED_CONTROL_STARTED"
                stream.activity(message=desired_sustained + " sustained hold at " + str(target["centerDistancePixels"]) + " px", level="info")
            else:
                hold_result = renew_hold(active_lease_control, active_lease_id)
            commanded_target = target
            commanded_control = active_lease_control
            emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="SUSTAINED", command=active_lease_control, command_result=hold_result, lease_id=active_lease_id, lease_state="ACTIVE", sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=reason, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            wait_for_sample_cadence(sample_started_ms)
            continue

        pulse = None if target["centerZone"]["inside"] else choose_pulse(target, no_movement_count)
        if pulse == None:
            commanded_target = None
            commanded_control = None
            wait_reason = "TRACKING_NEAR_CENTER" if mode == "TRACK" else "WAITING_FOR_STABLE_CENTER"
            emit_update(phase, sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=wait_reason, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            wait_for_sample_cadence(sample_started_ms)
            continue
        if command_count >= MAX_COMMANDS:
            emit_update(phase, sample, command_count, target, stable_confirmations, reason="COMMAND_LIMIT_REACHED")
            fail("Compass alignment exhausted the bounded command limit")

        command = pulse[0]
        hold_ms = pulse[1]
        command_result = action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": command, "holdMs": hold_ms})
        command_count += 1
        commanded_target = target
        commanded_control = command
        stream.activity(message=command + " pulse for " + str(hold_ms) + " ms at " + str(target["centerDistancePixels"]) + " px", level="info")
        emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="PULSE", command=command, command_result=command_result, command_hold_ms=hold_ms, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
        wait_for_sample_cadence(sample_started_ms)

    fail("Compass alignment exhausted the bounded sample limit")
