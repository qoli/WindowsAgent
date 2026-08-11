SAMPLE_CADENCE_MS = 1000
SUPERCRUISE_STATIC_TRACK_CADENCE_MS = 650
MAX_SLEEP_STEP_MS = 250
MAX_COMMANDS = 120
MAX_SAMPLES = 240
STABLE_CENTER_CONFIRMATIONS = 3
ALIGN_CENTER_RADIUS_PIXELS = 4.0
SUPERCRUISE_CENTER_RADIUS_PIXELS = 16.0
SUPERCRUISE_TRACK_CENTER_RADIUS_PIXELS = 20.0
SUPERCRUISE_CENTER_HYSTERESIS_PIXELS = 4.0
SUPERCRUISE_STATIC_TRACK_CENTER_RADIUS_PIXELS = 4.0
SUPERCRUISE_STATIC_TRACK_HYSTERESIS_PIXELS = 2.0
SUSTAINED_DISTANCE_PIXELS = 40
FINE_DISTANCE_PIXELS = 16
DIAGONAL_COMPONENT_MIN_PIXELS = 8
FINE_DIAGONAL_COMPONENT_MIN_PIXELS = 4
COARSE_HOLD_MS = 800
MEDIUM_HOLD_MS = 300
FINE_HOLD_MS = 120
SUPERCRUISE_FINE_DISTANCE_PIXELS = 40
SUPERCRUISE_FINE_HOLD_MS = 120
SUPERCRUISE_TRACK_FINE_HOLD_MS = 160
SUPERCRUISE_TRACK_NEAR_DISTANCE_PIXELS = 30
SUPERCRUISE_TRACK_NEAR_HOLD_MS = 120
SUPERCRUISE_STATIC_TRACK_FINE_DISTANCE_PIXELS = 6
SUPERCRUISE_STATIC_TRACK_FINE_HOLD_MS = 80
SUPERCRUISE_STATIC_TRACK_MID_HOLD_MS = 160
CENTER_ENTRY_BRAKE_MS = 100
SUPERCRUISE_CENTER_ENTRY_BRAKE_MS = 300
SUPERCRUISE_SUSTAINED_RELEASE_BRAKE_MS = 160
RECOVERY_HOLD_MS = 400
SUPERCRUISE_RECOVERY_HOLD_MS = 240
TRACK_TRANSITION_SETTLE_SAMPLES = 1
MIN_OBSERVED_MOVEMENT_PIXELS = 1
NO_MOVEMENT_LIMIT = 4
AWAY_TREND_LIMIT = 5
AMBIGUOUS_PRESENTATION_LIMIT = 12
TRANSIENT_MISSING_LIMIT = 3
MAX_COMPASS_DEADLINE_ERRORS = 5
MAX_COMPASS_NOT_VISIBLE_ERRORS = 5

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

def has_flag(value, bit_value):
    return (value // bit_value) % 2 == 1

def resolve_control_profile(requested_profile):
    if requested_profile != "AUTO":
        return {"profile": requested_profile, "source": "INPUT"}
    status = action.call(id="elite-dangerous/filesystem/status", inputs={})
    if status["state"] != "AVAILABLE":
        fail("automatic Compass control profile requires AVAILABLE Status.json evidence")
    if "data" not in status or "Flags" not in status["data"]:
        fail("automatic Compass control profile requires Status.json Flags")
    profile = "SUPERCRUISE_ASSIST" if has_flag(status["data"]["Flags"], 16) else "NORMAL_SPACE"
    return {"profile": profile, "source": "STATUS_JSON"}

def emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="NONE", command=None, command_result=None, reason=None, information=None, command_hold_ms=None, observed_movement_pixels=None, no_movement_count=0, distance_delta_pixels=None, away_trend_count=0, lease_id=None, lease_state=None, sample_started_ms=None, sample_duration_ms=None, sample_interval_ms=None, observation_error_code=None, observation_error=None):
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
            "observationErrorCode": observation_error_code,
            "observationError": observation_error,
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

def is_alignment_centered(target, radius_pixels):
    return (
        target["detected"] and
        target["presentation"] == "SOLID" and
        target["centerDistancePixels"] <= radius_pixels
    )

def choose_pulse(target, no_movement_count, fine_distance_pixels, fine_hold_ms, supercruise_profile):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    control = None
    if target["presentation"] == "HOLLOW":
        # Rear-projection offsets are not steering directions. Near the
        # antipode, use the proven pitch-up great-circle direction as a
        # bounded pulse so TRACK cannot hold through the following SOLID
        # observation and cross straight back to HOLLOW.
        control = "PITCH_UP"
    elif (
        target["presentation"] == "SOLID" and
        abs(offset_x) >= FINE_DIAGONAL_COMPONENT_MIN_PIXELS and
        abs(offset_y) >= FINE_DIAGONAL_COMPONENT_MIN_PIXELS and
        abs(offset_x) <= abs(offset_y) * 2 and
        abs(offset_y) <= abs(offset_x) * 2
    ):
        pitch = "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
        yaw = "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
        control = pitch + "_" + yaw
    else:
        control = choose_front_control(target)
    if control == None:
        return None
    distance = target["centerDistancePixels"]
    hold_ms = COARSE_HOLD_MS
    if distance <= fine_distance_pixels:
        hold_ms = fine_hold_ms
    elif distance <= SUSTAINED_DISTANCE_PIXELS:
        hold_ms = MEDIUM_HOLD_MS
    if no_movement_count >= 2:
        hold_ms = SUPERCRUISE_RECOVERY_HOLD_MS if supercruise_profile else RECOVERY_HOLD_MS
    return [control, hold_ms]

def choose_sustained_control(target):
    if target["presentation"] == "HOLLOW":
        # The rear projection crosses the antipode, so its signed offset is not
        # a trustworthy screen-space correction. Live gravity-well testing
        # showed that yaw can orbit a near-centered HOLLOW marker indefinitely,
        # while pitch immediately advances the same target toward the front
        # hemisphere. Use one fixed great-circle pitch direction until a fresh
        # Compass observation changes the topology to SOLID.
        return "PITCH_UP"
    if target["centerDistancePixels"] > SUSTAINED_DISTANCE_PIXELS:
        offset_x = target["offsetX"]
        offset_y = target["offsetY"]
        if abs(offset_x) >= DIAGONAL_COMPONENT_MIN_PIXELS and abs(offset_y) >= DIAGONAL_COMPONENT_MIN_PIXELS:
            pitch = "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
            yaw = "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
            return pitch + "_" + yaw
        return choose_front_control(target)
    return None

def choose_static_track_continuity_pulse(target):
    # The STATIC TRACK front topology is established by its required ALIGN.
    # Presentation flicker must not select rear recovery or a coarse pulse.
    # Use the dominant continuous screen axis and keep every correction
    # bounded, even when the noisy projected marker temporarily moves far.
    control = choose_front_control(target)
    if control == None:
        return None
    distance = target["centerDistancePixels"]
    hold_ms = SUPERCRUISE_TRACK_FINE_HOLD_MS
    if distance <= SUPERCRUISE_STATIC_TRACK_FINE_DISTANCE_PIXELS:
        hold_ms = SUPERCRUISE_STATIC_TRACK_FINE_HOLD_MS
    elif distance <= SUPERCRUISE_TRACK_NEAR_DISTANCE_PIXELS:
        hold_ms = SUPERCRUISE_STATIC_TRACK_MID_HOLD_MS
    return [control, hold_ms]

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

def pulse_control(control, hold_ms):
    if not is_vector_control(control):
        return action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": control, "holdMs": hold_ms})
    start_result = start_hold(control)
    lease_id = start_result["leaseId"]
    register_hold_failure(control, lease_id)
    remaining = hold_ms
    while remaining > 0:
        step = remaining if remaining <= MAX_SLEEP_STEP_MS else MAX_SLEEP_STEP_MS
        task.sleep(milliseconds=step)
        remaining -= step
    stop_result = stop_hold(control, lease_id)
    action.clear_on_failure()
    return {"start": start_result, "stop": stop_result, "holdMs": hold_ms}

def register_hold_failure(control, lease_id):
    if is_vector_control(control):
        action.on_failure(id="elite-dangerous/ship-attitude-vector-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id}, critical=True, timeout_milliseconds=2000)
    else:
        action.on_failure(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id}, critical=True, timeout_milliseconds=2000)

def wait_for_sample_cadence(sample_started_ms, cadence_ms):
    remaining = cadence_ms - (task.elapsed_milliseconds() - sample_started_ms)
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
    target_motion = ctx.inputs["targetMotion"] if "targetMotion" in ctx.inputs else "MOVING"
    stop_before_align = ctx.inputs["stopBeforeAlign"] if "stopBeforeAlign" in ctx.inputs else True
    requested_control_profile = ctx.inputs["controlProfile"] if "controlProfile" in ctx.inputs else "AUTO"
    profile_resolution = resolve_control_profile(requested_control_profile)
    control_profile = profile_resolution["profile"]
    control_profile_source = profile_resolution["source"]
    supercruise_profile = control_profile == "SUPERCRUISE_ASSIST"
    stable_confirmations_required = 2 if supercruise_profile else STABLE_CENTER_CONFIRMATIONS
    alignment_radius = SUPERCRUISE_CENTER_RADIUS_PIXELS if control_profile == "SUPERCRUISE_ASSIST" else ALIGN_CENTER_RADIUS_PIXELS
    alignment_hysteresis = SUPERCRUISE_CENTER_HYSTERESIS_PIXELS if control_profile == "SUPERCRUISE_ASSIST" else 0.0
    if control_profile == "SUPERCRUISE_ASSIST" and mode == "TRACK":
        alignment_radius = SUPERCRUISE_TRACK_CENTER_RADIUS_PIXELS
        if target_motion == "STATIC":
            alignment_radius = SUPERCRUISE_STATIC_TRACK_CENTER_RADIUS_PIXELS
            alignment_hysteresis = SUPERCRUISE_STATIC_TRACK_HYSTERESIS_PIXELS
    fine_distance = SUPERCRUISE_FINE_DISTANCE_PIXELS if control_profile == "SUPERCRUISE_ASSIST" else FINE_DISTANCE_PIXELS
    fine_hold = FINE_HOLD_MS
    if control_profile == "SUPERCRUISE_ASSIST":
        fine_hold = SUPERCRUISE_TRACK_FINE_HOLD_MS if mode == "TRACK" else SUPERCRUISE_FINE_HOLD_MS
    sample_limit = tracking_samples if mode == "TRACK" else MAX_SAMPLES
    sample_cadence_ms = SUPERCRUISE_STATIC_TRACK_CADENCE_MS if mode == "TRACK" and target_motion == "STATIC" and supercruise_profile else SAMPLE_CADENCE_MS

    stream.activity(message="Compass control profile " + control_profile + " selected from " + control_profile_source, level="info")
    if mode == "TRACK":
        stream.activity(message="Target motion profile " + target_motion + " selected", level="info")

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
    commanded_hold_ms = None
    no_movement_count = 0
    pitch_no_movement_count = 0
    away_trend_count = 0
    active_lease_id = None
    active_lease_control = None
    previous_sample_started_ms = None
    ambiguous_presentation_count = 0
    last_clear_presentation = None
    transition_control = None
    transient_missing_count = 0
    target_seen = False
    compass_deadline_error_count = 0
    compass_not_visible_error_count = 0
    track_recovery_control = None
    track_command_cooldown = 0
    last_track_control = None

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
            error_text = attempt["error"]
            bounded_error = error_text if len(error_text) <= 512 else error_text[:512]
            if attempt["errorCode"] == "JOB_DEADLINE_EXCEEDED":
                compass_deadline_error_count += 1
                emit_update("OBSERVATION_ERROR", sample, command_count, empty_target(), stable_confirmations, lease_id=active_lease_id, lease_state="RELEASED" if active_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="COMPASS_DEADLINE_RETRY", observation_error_code=attempt["errorCode"], observation_error=bounded_error)
                if compass_deadline_error_count > MAX_COMPASS_DEADLINE_ERRORS:
                    fail("Compass deadline error limit exceeded after five skipped errors: " + error_text)
                wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
                continue
            if attempt["errorCode"] == "COMPASS_NOT_VISIBLE":
                compass_not_visible_error_count += 1
                emit_update("OBSERVATION_ERROR", sample, command_count, empty_target(), stable_confirmations, lease_id=active_lease_id, lease_state="RELEASED" if active_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="COMPASS_NOT_VISIBLE_RETRY", observation_error_code=attempt["errorCode"], observation_error=bounded_error)
                if compass_not_visible_error_count > MAX_COMPASS_NOT_VISIBLE_ERRORS:
                    fail("Compass remained invisible after five skipped observations: " + error_text)
                wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
                continue
            emit_update("OBSERVATION_ERROR", sample, command_count, empty_target(), stable_confirmations, lease_id=active_lease_id, lease_state="RELEASED" if active_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="COMPASS_OBSERVATION_FAILED", observation_error_code=attempt["errorCode"], observation_error=bounded_error)
            fail("Compass observation failed: " + attempt["error"])

        observation = attempt["output"]
        compass_not_visible_error_count = 0
        target = observation["target"]
        final_observation = observation
        observed_movement = None
        distance_delta = None
        if commanded_target != None and target["detected"]:
            observed_movement = max(
                abs(target["offsetX"] - commanded_target["offsetX"]),
                abs(target["offsetY"] - commanded_target["offsetY"]),
            )
            if observed_movement < MIN_OBSERVED_MOVEMENT_PIXELS:
                no_movement_count += 1
                if mode == "TRACK" and no_movement_count > 2:
                    no_movement_count = 2
                if commanded_control in ["PITCH_UP", "PITCH_DOWN"]:
                    pitch_no_movement_count += 1
                else:
                    pitch_no_movement_count = 0
            else:
                no_movement_count = 0
                pitch_no_movement_count = 0
            distance_delta = target["centerDistancePixels"] - commanded_target["centerDistancePixels"]
            if mode == "ALIGN" and commanded_target["presentation"] == "SOLID" and target["presentation"] == "SOLID":
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
            commanded_hold_ms = None
            # A sustained lease spans several Compass frames, so its movement
            # counter cannot be reused to size the first bounded pulse after
            # release. Start the new control mode with fresh progress evidence.
            no_movement_count = 0
            pitch_no_movement_count = 0
            away_trend_count = 0
            transient_missing_count += 1
            reason = "TARGET_NOT_DETECTED" if not target_seen else "TARGET_NOT_DETECTED_TRANSIENT"
            emit_update("OBSERVING", sample, command_count, target, 0, lease_id=released_lease_id, lease_state="RELEASED" if released_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=reason)
            if not target_seen or transient_missing_count >= TRANSIENT_MISSING_LIMIT:
                fail("Compass target is not detected; establish the intended Station target lock first")
            wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
            continue
        if mode == "TRACK" and not target_seen and target["presentation"] == "HOLLOW":
            emit_update("OBSERVING", sample, command_count, target, 0, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="TRACK_REQUIRES_SOLID_INITIAL_TARGET")
            fail("TRACK requires an initial SOLID Compass target; run ALIGN immediately before TRACK so rear recovery has a known prior control direction")
        target_seen = True
        transient_missing_count = 0
        if target["presentation"] == "UNKNOWN":
            ambiguous_presentation_count += 1
            released_control = active_lease_control
            if released_control != None:
                transition_control = released_control
            release_lease(active_lease_id, active_lease_control, "OBSERVING", sample, command_count, target, 0, "SUSTAINED_CONTROL_RELEASED_FOR_AMBIGUOUS_OBSERVATION", sample_started_ms, sample_duration_ms, sample_interval_ms)
            released_lease_id = active_lease_id
            active_lease_id = None
            active_lease_control = None
            commanded_target = None
            commanded_control = None
            commanded_hold_ms = None
            emit_update("OBSERVING", sample, command_count, target, 0, lease_id=released_lease_id, lease_state="RELEASED" if released_lease_id != None else None, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="TARGET_PRESENTATION_UNKNOWN")
            transition_command = transition_control if mode == "ALIGN" and supercruise_profile and last_clear_presentation == "HOLLOW" else None
            brake_control = opposite_control(released_control) if transition_command == None and mode == "ALIGN" and released_control != None else None
            if transition_command != None:
                if command_count >= MAX_COMMANDS:
                    emit_update("OBSERVING", sample, command_count, target, 0, reason="COMMAND_LIMIT_REACHED")
                    fail("Compass alignment exhausted the bounded command limit")
                transition_result = pulse_control(transition_command, fine_hold)
                command_count += 1
                stream.activity(message=transition_command + " rear-to-front transition pulse for " + str(fine_hold) + " ms", level="info")
                emit_update("OBSERVING", sample, command_count, target, 0, control_mode="PULSE", command=transition_command, command_result=transition_result, command_hold_ms=fine_hold, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="AMBIGUOUS_REAR_TRANSITION_CONTINUE")
            elif brake_control != None:
                if command_count >= MAX_COMMANDS:
                    emit_update("OBSERVING", sample, command_count, target, 0, reason="COMMAND_LIMIT_REACHED")
                    fail("Compass alignment exhausted the bounded command limit")
                brake_result = pulse_control(brake_control, FINE_HOLD_MS)
                command_count += 1
                stream.activity(message=brake_control + " transition brake for " + str(FINE_HOLD_MS) + " ms", level="info")
                emit_update("OBSERVING", sample, command_count, target, 0, control_mode="PULSE", command=brake_control, command_result=brake_result, command_hold_ms=FINE_HOLD_MS, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="AMBIGUOUS_TRANSITION_BRAKE")
            if ambiguous_presentation_count >= AMBIGUOUS_PRESENTATION_LIMIT:
                fail("Compass target hollow or solid presentation remained ambiguous")
            wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
            continue
        ambiguous_presentation_count = 0
        previous_clear_presentation = last_clear_presentation
        static_track_presentation_continuity = (
            mode == "TRACK" and
            target_motion == "STATIC" and
            supercruise_profile and
            last_clear_presentation == "SOLID" and
            target["presentation"] == "HOLLOW"
        )
        control_target = target
        if static_track_presentation_continuity:
            # TRACK starts from a separately completed ALIGN. A static target
            # cannot physically move from the front to the rear hemisphere
            # after the bounded pulses used here. Live approach
            # evidence showed isolated HOLLOW classifications at 22 px while
            # the marker retained continuous screen geometry; treating that
            # frame as a rear-antipode transition started a sustained reversal
            # and caused the actual loss of alignment. Preserve the observed
            # presentation in event output, but latch the established front
            # topology for bounded screen-space correction throughout this
            # invocation.
            control_target = {
                "detected": target["detected"],
                "presentation": "SOLID",
                "hemisphere": "FRONT",
                "offsetX": target["offsetX"],
                "offsetY": target["offsetY"],
                "centerDistancePixels": target["centerDistancePixels"],
                "centerZone": target["centerZone"],
            }
        else:
            last_clear_presentation = target["presentation"]
        if mode == "TRACK":
            if not static_track_presentation_continuity and target["presentation"] == "HOLLOW" and previous_clear_presentation == "SOLID":
                reverse_control = opposite_control(last_track_control) if last_track_control != None else None
                track_recovery_control = reverse_control if reverse_control != None else "PITCH_UP"
                # Crossing from front to rear proves that the previous command
                # passed the antipode. Reverse immediately instead of honoring
                # the ordinary post-command cooldown.
                track_command_cooldown = 0
            elif target["presentation"] == "SOLID" and previous_clear_presentation == "HOLLOW":
                track_recovery_control = None
                # Let residual angular velocity settle for one current Compass
                # frame before choosing a new front-projection correction.
                track_command_cooldown = TRACK_TRANSITION_SETTLE_SAMPLES
        if control_target["presentation"] == "SOLID":
            transition_control = None
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

        active_alignment_radius = alignment_radius + alignment_hysteresis if stable_confirmations > 0 else alignment_radius
        alignment_centered = is_alignment_centered(control_target, active_alignment_radius)
        phase = "TURNING_TO_FRONT"
        if control_target["presentation"] == "SOLID":
            if alignment_centered:
                phase = "VERIFYING_CENTER"
            elif target["centerDistancePixels"] > SUSTAINED_DISTANCE_PIXELS:
                phase = "COARSE_ALIGN"
            else:
                phase = "FINE_ALIGN"

        desired_sustained = None
        if mode == "ALIGN":
            desired_sustained = choose_sustained_control(control_target)
        elif control_target["presentation"] == "HOLLOW":
            desired_sustained = track_recovery_control if track_recovery_control != None else "PITCH_UP"
        if active_lease_id != None and desired_sustained != active_lease_control:
            released_control = active_lease_control
            released_id = active_lease_id
            release_result = stop_hold(released_control, released_id)
            action.clear_on_failure()
            active_lease_id = None
            active_lease_control = None
            commanded_target = None
            commanded_control = None
            commanded_hold_ms = None
            # The sustained lease's progress counter describes continuous
            # control and must not promote the first post-release pulse into
            # recovery strength.
            no_movement_count = 0
            pitch_no_movement_count = 0
            away_trend_count = 0
            emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="SUSTAINED", command=released_control, command_result=release_result, lease_id=released_id, lease_state="RELEASED", sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="SUSTAINED_CONTROL_RELEASED", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            if mode == "ALIGN" and supercruise_profile and alignment_centered:
                brake_control = opposite_control(released_control)
                if brake_control != None:
                    if command_count >= MAX_COMMANDS:
                        emit_update("VERIFYING_CENTER", sample, command_count, target, stable_confirmations, reason="COMMAND_LIMIT_REACHED")
                        fail("Compass alignment exhausted the bounded command limit")
                    brake_result = pulse_control(brake_control, SUPERCRUISE_SUSTAINED_RELEASE_BRAKE_MS)
                    command_count += 1
                    stream.activity(message=brake_control + " sustained-release brake for " + str(SUPERCRUISE_SUSTAINED_RELEASE_BRAKE_MS) + " ms", level="info")
                    emit_update("VERIFYING_CENTER", sample, command_count, target, 0, control_mode="PULSE", command=brake_control, command_result=brake_result, command_hold_ms=SUPERCRUISE_SUSTAINED_RELEASE_BRAKE_MS, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="SUPERCRUISE_SUSTAINED_RELEASE_BRAKE", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
                    # This command mutates attitude after the current Compass frame.
                    # Never complete from the pre-brake SOLID sample: live evidence
                    # showed a 300 ms brake could cross the antipode and leave the
                    # next frame HOLLOW even though this frame was centered.
                    stable_confirmations = 0
                    emit_update("VERIFYING_CENTER", sample, command_count, target, 0, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="WAITING_POST_BRAKE_OBSERVATION")
                    wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
                    continue

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

        if alignment_centered:
            stable_confirmations += 1
            center_contact_count += 1
            if stable_confirmations > max_consecutive_center:
                max_consecutive_center = stable_confirmations
        else:
            stable_confirmations = 0

        should_brake_center_entry = (
            mode == "ALIGN" and
            alignment_centered and
            commanded_target != None and
            commanded_control != None and
            commanded_target["centerDistancePixels"] > fine_distance
        )
        if should_brake_center_entry:
            brake_control = opposite_control(commanded_control)
            brake_applied = False
            if brake_control != None:
                if command_count >= MAX_COMMANDS:
                    emit_update("VERIFYING_CENTER", sample, command_count, target, stable_confirmations, reason="COMMAND_LIMIT_REACHED")
                    fail("Compass alignment exhausted the bounded command limit")
                brake_hold_ms = SUPERCRUISE_CENTER_ENTRY_BRAKE_MS if supercruise_profile else CENTER_ENTRY_BRAKE_MS
                brake_result = pulse_control(brake_control, brake_hold_ms)
                command_count += 1
                brake_applied = True
                stream.activity(message=brake_control + " center-entry brake for " + str(brake_hold_ms) + " ms", level="info")
                emit_update("VERIFYING_CENTER", sample, command_count, target, 0, control_mode="PULSE", command=brake_control, command_result=brake_result, command_hold_ms=brake_hold_ms, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="CENTER_ENTRY_BRAKE", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            commanded_target = None
            commanded_control = None
            commanded_hold_ms = None
            no_movement_count = 0
            pitch_no_movement_count = 0
            away_trend_count = 0
            if brake_applied:
                # Every brake changes attitude after this Compass observation.
                # Require a fresh post-command frame in both normal space and
                # Supercruise instead of completing from stale geometry.
                stable_confirmations = 0
                emit_update("VERIFYING_CENTER", sample, command_count, target, 0, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="WAITING_POST_BRAKE_OBSERVATION")
                wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
                continue

        if mode == "ALIGN" and stable_confirmations >= stable_confirmations_required:
            emit_update("COMPLETED", sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="SOLID_TARGET_STABLY_CENTERED")
            stream.activity(message="Station target aligned", level="info")
            return {
                "schemaVersion": 2,
                "task": "ALIGN_STATION_TARGET",
                "mode": mode,
                "targetMotion": target_motion,
                "controlProfile": control_profile,
                "controlProfileSource": control_profile_source,
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
            released_id = active_lease_id
            released_control = active_lease_control
            if released_id != None:
                release_result = stop_hold(released_control, released_id)
                action.clear_on_failure()
                active_lease_id = None
                active_lease_control = None
                emit_update("TRACKING_WINDOW_COMPLETED", sample, command_count, target, stable_confirmations, control_mode="SUSTAINED", command=released_control, command_result=release_result, lease_id=released_id, lease_state="RELEASED", sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="SUSTAINED_CONTROL_RELEASED_AT_TRACKING_WINDOW")
            emit_update("TRACKING_WINDOW_COMPLETED", sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="BOUNDED_TRACKING_WINDOW_COMPLETED", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            stream.activity(message="Moving-target tracking window completed", level="info")
            return {
                "schemaVersion": 2,
                "task": "ALIGN_STATION_TARGET",
                "mode": mode,
                "targetMotion": target_motion,
                "controlProfile": control_profile,
                "controlProfileSource": control_profile_source,
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
                if mode == "TRACK":
                    last_track_control = desired_sustained
                register_hold_failure(active_lease_control, active_lease_id)
                command_count += 1
                reason = "SUSTAINED_CONTROL_STARTED"
                stream.activity(message=desired_sustained + " sustained hold at " + str(target["centerDistancePixels"]) + " px", level="info")
            else:
                hold_result = renew_hold(active_lease_control, active_lease_id)
            commanded_target = target
            commanded_control = active_lease_control
            commanded_hold_ms = None
            emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="SUSTAINED", command=active_lease_control, command_result=hold_result, lease_id=active_lease_id, lease_state="ACTIVE", sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=reason, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
            continue

        # The Compass package's generic four-pixel center zone is appropriate
        # for normal-space tracking, but live Supercruise approach evidence
        # shows it creates an avoidable correction cycle around 8-12 px. Use
        # the calibrated Supercruise alignment Gate for both ALIGN and TRACK.
        tracking_centered = alignment_centered if supercruise_profile else target["centerZone"]["inside"]
        track_entered_hysteresis_after_progress = (
            mode == "TRACK" and
            supercruise_profile and
            control_target["presentation"] == "SOLID" and
            commanded_target != None and
            distance_delta != None and
            distance_delta < 0 and
            target["centerDistancePixels"] <= alignment_radius + alignment_hysteresis
        )
        if track_entered_hysteresis_after_progress:
            # A fresh post-command frame proves both direction and useful
            # movement. Once that pulse enters the calibrated hysteresis band,
            # preserve the angular gain for one frame instead of immediately
            # stacking a second pulse and crossing to HOLLOW.
            stable_confirmations = 1
            commanded_target = None
            commanded_control = None
            commanded_hold_ms = None
            emit_update(phase, sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="TRACKING_POST_COMMAND_SETTLE", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
            continue
        if mode == "TRACK" and track_command_cooldown > 0:
            track_command_cooldown -= 1
            commanded_target = None
            commanded_control = None
            commanded_hold_ms = None
            emit_update(phase, sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason="TRACKING_COMMAND_COOLDOWN", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
            continue
        pulse = None
        pulse_reason = None
        if not (alignment_centered if mode == "ALIGN" else tracking_centered):
            if static_track_presentation_continuity:
                pulse = choose_static_track_continuity_pulse(control_target)
                pulse_reason = "STATIC_TRACK_PRESENTATION_CONTINUITY"
            elif mode == "TRACK" and control_target["presentation"] == "HOLLOW" and track_recovery_control != None:
                track_hold_ms = SUPERCRUISE_RECOVERY_HOLD_MS if supercruise_profile and no_movement_count >= 2 else fine_hold
                pulse = [track_recovery_control, track_hold_ms]
            else:
                pulse = choose_pulse(control_target, no_movement_count, fine_distance, fine_hold, supercruise_profile)
                if (
                    mode == "TRACK" and
                    supercruise_profile and
                    control_target["presentation"] == "SOLID" and
                    target["centerDistancePixels"] <= SUPERCRUISE_TRACK_NEAR_DISTANCE_PIXELS and
                    no_movement_count < 2 and
                    pulse != None
                ):
                    pulse = [pulse[0], SUPERCRUISE_TRACK_NEAR_HOLD_MS]
                if (
                    mode == "TRACK" and
                    target_motion == "STATIC" and
                    supercruise_profile and
                    control_target["presentation"] == "SOLID" and
                    target["centerDistancePixels"] <= SUPERCRUISE_TRACK_NEAR_DISTANCE_PIXELS and
                    pulse != None
                ):
                    static_hold_ms = SUPERCRUISE_STATIC_TRACK_FINE_HOLD_MS if target["centerDistancePixels"] <= SUPERCRUISE_STATIC_TRACK_FINE_DISTANCE_PIXELS else SUPERCRUISE_STATIC_TRACK_MID_HOLD_MS
                    pulse = [pulse[0], static_hold_ms]
        if pulse == None:
            commanded_target = None
            commanded_control = None
            commanded_hold_ms = None
            wait_reason = "TRACKING_NEAR_CENTER" if mode == "TRACK" else "WAITING_FOR_STABLE_CENTER"
            emit_update(phase, sample, command_count, target, stable_confirmations, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=wait_reason, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)
            continue
        if command_count >= MAX_COMMANDS:
            emit_update(phase, sample, command_count, target, stable_confirmations, reason="COMMAND_LIMIT_REACHED")
            fail("Compass alignment exhausted the bounded command limit")

        command = pulse[0]
        hold_ms = pulse[1]
        command_result = pulse_control(command, hold_ms)
        command_count += 1
        commanded_target = target
        commanded_control = command
        commanded_hold_ms = hold_ms
        if mode == "TRACK":
            last_track_control = command
        stream.activity(message=command + " pulse for " + str(hold_ms) + " ms at " + str(target["centerDistancePixels"]) + " px", level="info")
        emit_update(phase, sample, command_count, target, stable_confirmations, control_mode="PULSE", command=command, command_result=command_result, command_hold_ms=hold_ms, sample_started_ms=sample_started_ms, sample_duration_ms=sample_duration_ms, sample_interval_ms=sample_interval_ms, reason=pulse_reason, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
        wait_for_sample_cadence(sample_started_ms, sample_cadence_ms)

    fail("Compass alignment exhausted the bounded sample limit")
