POLL_MS = 500
GATE_CONFIRMATIONS = 2
GATE_OBSERVATION_LIMIT = 6
CLEAR_CONFIRMATIONS = 2
TURN_PULSE_MS = 800
MAX_TURN_PULSES = 16
MAX_NO_PROGRESS_PULSES = 4
MIN_PROJECTION_PROGRESS_PIXELS = 2.0
BYPASS_OVERSHOOT_PIXELS = 96.0
MAX_BYPASS_FLIGHT_SAMPLES = 120

def observe_flight():
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    classified = action.call(id="elite-dangerous/flight-status", inputs=raw)
    return {"state": classified["flightStatus"]["state"], "text": raw["text"]}

def emit_update(phase, sample, target_name, flight=None, direction=None, target=None, projection=None, turn_pulses=0, no_progress=0, clear_confirmations=0, commanded_throttle=None, last_command=None, reason=None):
    stream.emit(
        type="action.clear-supercruise-assist-line-of-sight.update",
        payload={
            "phase": phase,
            "sample": sample,
            "targetName": target_name,
            "flightStatus": None if flight == None else flight["state"],
            "flightPromptText": None if flight == None else flight["text"],
            "directionState": None if direction == None else direction["state"],
            "control": None if direction == None else direction["control"],
            "targetPresentation": None if target == None else target["presentation"],
            "targetOffsetX": None if target == None else target["offsetX"],
            "targetOffsetY": None if target == None else target["offsetY"],
            "projectionPixels": projection,
            "turnPulses": turn_pulses,
            "noProgressPulses": no_progress,
            "clearConfirmations": clear_confirmations,
            "commandedThrottle": commanded_throttle,
            "lastCommand": last_command,
            "reason": reason,
        },
    )

def clear_status(state):
    return state in [
        "UNKNOWN",
        "SUPERCRUISE",
        "SUPERCRUISE_ASSIST_ACTIVE",
        "FSD_ALIGNMENT_REQUIRED",
        "SAFE_DISENGAGE_READY",
    ]

def vector_control(control):
    return "_YAW_" in control

def install_failure_compensation(control=None, lease_id=None):
    action.clear_on_failure()
    action.on_failure(
        id="elite-dangerous/set-throttle",
        inputs={"percent": 0},
        critical=True,
        timeout_milliseconds=2000,
    )
    if control != None and lease_id != None:
        action.on_failure(
            id="elite-dangerous/ship-attitude-vector-hold",
            inputs={"operation": "STOP", "control": control, "leaseId": lease_id},
            critical=True,
            timeout_milliseconds=2000,
        )

def pulse(control):
    if not vector_control(control):
        return action.call(
            id="elite-dangerous/ship-attitude-control",
            inputs={"control": control, "holdMs": TURN_PULSE_MS},
        )
    started = action.call(
        id="elite-dangerous/ship-attitude-vector-hold",
        inputs={"operation": "START", "control": control},
    )
    lease_id = started["leaseId"]
    install_failure_compensation(control, lease_id)
    task.sleep(milliseconds=TURN_PULSE_MS)
    stopped = action.call(
        id="elite-dangerous/ship-attitude-vector-hold",
        inputs={"operation": "STOP", "control": control, "leaseId": lease_id},
    )
    install_failure_compensation()
    return stopped

def projection(control, target):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    if control == "YAW_RIGHT":
        return offset_x
    if control == "YAW_LEFT":
        return -offset_x
    if control == "PITCH_DOWN":
        return offset_y
    if control == "PITCH_UP":
        return -offset_y
    horizontal = offset_x if "YAW_RIGHT" in control else -offset_x
    vertical = offset_y if "PITCH_DOWN" in control else -offset_y
    return min(horizontal, vertical)

def main(ctx):
    target_name = ctx.inputs["targetName"]
    sample = 0
    gate_confirmations = 0
    last_flight = None
    for _ in range(GATE_OBSERVATION_LIMIT):
        sample += 1
        last_flight = observe_flight()
        if last_flight["state"] == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
            gate_confirmations += 1
        elif clear_status(last_flight["state"]):
            gate_confirmations = 0
        else:
            fail("unexpected known flight status while confirming line-of-sight Gate: " + last_flight["state"])
        emit_update(
            "CONFIRMING_GATE",
            sample,
            target_name,
            flight=last_flight,
            clear_confirmations=0,
            reason="LINE_OF_SIGHT_GATE_" + str(gate_confirmations) + "_OF_" + str(GATE_CONFIRMATIONS),
        )
        if gate_confirmations >= GATE_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if gate_confirmations < GATE_CONFIRMATIONS:
        fail("LINE_OF_SIGHT_GATE_NOT_STABLE: prompt was not confirmed twice")

    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    install_failure_compensation()
    emit_update(
        "STOPPING_DIRECT_APPROACH",
        sample,
        target_name,
        flight=last_flight,
        commanded_throttle=0,
        last_command="SET_THROTTLE_0",
        reason="LINE_OF_SIGHT_GATE_CONFIRMED:" + throttle["control"],
    )

    direction_output = action.call(
        id="elite-dangerous/supercruise-line-of-sight-direction",
        inputs={"targetName": target_name},
    )
    direction = direction_output["direction"]
    emit_update(
        "SELECTING_DIRECTION",
        sample,
        target_name,
        flight=last_flight,
        direction=direction,
        projection=direction["initialProjectionPixels"],
        commanded_throttle=0,
        reason=direction["reason"],
    )
    if direction["state"] != "READY":
        fail("LINE_OF_SIGHT_DIRECTION_UNKNOWN: " + direction["reason"])

    control = direction["control"]
    previous_projection = direction["initialProjectionPixels"]
    no_progress = 0
    clear_confirmations = 0
    turn_pulses = 0
    bypass_reached = False
    for _ in range(MAX_TURN_PULSES):
        turn_pulses += 1
        pulse(control)
        sample += 1
        flight = observe_flight()
        if flight["state"] != "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
            if not clear_status(flight["state"]):
                fail("unexpected known flight status while turning around occlusion: " + flight["state"])
            clear_confirmations += 1
            emit_update(
                "VERIFYING_CLEAR",
                sample,
                target_name,
                flight=flight,
                direction=direction,
                projection=previous_projection,
                turn_pulses=turn_pulses,
                no_progress=no_progress,
                clear_confirmations=clear_confirmations,
                commanded_throttle=0,
                last_command=control,
                reason="LINE_OF_SIGHT_PROMPT_CLEAR_DURING_TURN",
            )
            if clear_confirmations >= CLEAR_CONFIRMATIONS:
                action.clear_on_failure()
                stream.activity(message="Supercruise Assist line of sight cleared", level="info")
                return {
                    "schemaVersion": 1,
                    "task": "CLEAR_SUPERCRUISE_ASSIST_LINE_OF_SIGHT",
                    "completed": True,
                    "targetName": target_name,
                    "control": control,
                    "turnPulses": turn_pulses,
                    "bypassFlightSamples": 0,
                    "finalFlightStatus": flight["state"],
                    "sampleCount": sample,
                }
            # A first clear sample revokes steering authority immediately. The
            # second Gate sample is read without another attitude pulse.
            task.sleep(milliseconds=POLL_MS)
            sample += 1
            verified = observe_flight()
            if verified["state"] == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
                clear_confirmations = 0
                emit_update(
                    "TURNING_TO_BYPASS",
                    sample,
                    target_name,
                    flight=verified,
                    direction=direction,
                    projection=previous_projection,
                    turn_pulses=turn_pulses,
                    no_progress=no_progress,
                    clear_confirmations=0,
                    commanded_throttle=0,
                    reason="LINE_OF_SIGHT_PROMPT_RETURNED_DURING_NO_INPUT_VERIFICATION",
                )
                continue
            if not clear_status(verified["state"]):
                fail("unexpected known flight status while verifying line-of-sight clear: " + verified["state"])
            clear_confirmations += 1
            emit_update(
                "VERIFYING_CLEAR",
                sample,
                target_name,
                flight=verified,
                direction=direction,
                projection=previous_projection,
                turn_pulses=turn_pulses,
                no_progress=no_progress,
                clear_confirmations=clear_confirmations,
                commanded_throttle=0,
                reason="LINE_OF_SIGHT_PROMPT_CLEAR_WITHOUT_ADDITIONAL_STEERING",
            )
            action.clear_on_failure()
            stream.activity(message="Supercruise Assist line of sight cleared", level="info")
            return {
                "schemaVersion": 1,
                "task": "CLEAR_SUPERCRUISE_ASSIST_LINE_OF_SIGHT",
                "completed": True,
                "targetName": target_name,
                "control": control,
                "turnPulses": turn_pulses,
                "bypassFlightSamples": 0,
                "finalFlightStatus": verified["state"],
                "sampleCount": sample,
            }
        clear_confirmations = 0

        observed = action.call(
            id="elite-dangerous/supercruise-target-position",
            inputs={"targetName": target_name},
        )["target"]
        if observed["state"] != "DETECTED":
            fail("LINE_OF_SIGHT_TARGET_LOST_AFTER_TURN: " + observed["reason"])
        current_projection = projection(control, observed)
        progress = previous_projection - current_projection
        if progress < MIN_PROJECTION_PROGRESS_PIXELS:
            no_progress += 1
        else:
            no_progress = 0
        emit_update(
            "TURNING_TO_BYPASS",
            sample,
            target_name,
            flight=flight,
            direction=direction,
            target=observed,
            projection=current_projection,
            turn_pulses=turn_pulses,
            no_progress=no_progress,
            commanded_throttle=0,
            last_command=control,
            reason="FIXED_INITIAL_DIRECTION_UNTIL_CENTRE_CROSSING_OVERSHOOT",
        )
        if no_progress >= MAX_NO_PROGRESS_PULSES:
            fail("LINE_OF_SIGHT_TURN_NO_PROGRESS: focus-frame projection did not move")
        previous_projection = current_projection
        if observed["presentation"] == "SOLID" or current_projection <= -BYPASS_OVERSHOOT_PIXELS:
            bypass_reached = True
            break
        task.sleep(milliseconds=POLL_MS)
    if not bypass_reached:
        fail("LINE_OF_SIGHT_TURN_LIMIT: bypass overshoot was not reached")

    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
    emit_update(
        "BYPASS_FLIGHT",
        sample,
        target_name,
        flight=last_flight,
        direction=direction,
        projection=previous_projection,
        turn_pulses=turn_pulses,
        commanded_throttle=75,
        last_command="SET_THROTTLE_75",
        reason="TANGENTIAL_BYPASS_AFTER_OVERSHOOT:" + throttle["control"],
    )
    bypass_flight_samples = 0
    clear_confirmations = 0
    for _ in range(MAX_BYPASS_FLIGHT_SAMPLES):
        bypass_flight_samples += 1
        sample += 1
        flight = observe_flight()
        if flight["state"] == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED":
            clear_confirmations = 0
        elif clear_status(flight["state"]):
            clear_confirmations += 1
        else:
            fail("unexpected known flight status during line-of-sight bypass flight: " + flight["state"])
        emit_update(
            "VERIFYING_CLEAR" if clear_confirmations > 0 else "BYPASS_FLIGHT",
            sample,
            target_name,
            flight=flight,
            direction=direction,
            projection=previous_projection,
            turn_pulses=turn_pulses,
            clear_confirmations=clear_confirmations,
            commanded_throttle=75,
            reason="WAITING_FOR_LINE_OF_SIGHT_PROMPT_TO_CLEAR_TWICE",
        )
        if clear_confirmations >= CLEAR_CONFIRMATIONS:
            stopped = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            action.clear_on_failure()
            emit_update(
                "COMPLETED",
                sample,
                target_name,
                flight=flight,
                direction=direction,
                projection=previous_projection,
                turn_pulses=turn_pulses,
                clear_confirmations=clear_confirmations,
                commanded_throttle=0,
                last_command="SET_THROTTLE_0",
                reason="LINE_OF_SIGHT_PROMPT_CLEAR_TWICE:" + stopped["control"],
            )
            stream.activity(message="Supercruise Assist line of sight cleared", level="info")
            return {
                "schemaVersion": 1,
                "task": "CLEAR_SUPERCRUISE_ASSIST_LINE_OF_SIGHT",
                "completed": True,
                "targetName": target_name,
                "control": control,
                "turnPulses": turn_pulses,
                "bypassFlightSamples": bypass_flight_samples,
                "finalFlightStatus": flight["state"],
                "sampleCount": sample,
            }
        task.sleep(milliseconds=POLL_MS)
    fail("LINE_OF_SIGHT_PROMPT_PERSISTED: bounded tangential bypass did not clear the Gate")
