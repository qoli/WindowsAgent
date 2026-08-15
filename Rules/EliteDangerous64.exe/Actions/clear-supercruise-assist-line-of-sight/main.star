POLL_MS = 500
GATE_CONFIRMATIONS = 2
GATE_OBSERVATION_LIMIT = 6
DIRECTION_CONFIRMATIONS = 2
TURN_PULSE_MS = 800
FIXED_TURN_PULSES = 8
FIXED_TURN_DURATION_MS = 6400
SEPARATION_DURATION_MS = 30000
SEPARATION_SAMPLES = 60
CLEAR_CONFIRMATIONS = 2
CLEAR_OBSERVATION_LIMIT = 8

def observe_flight():
    classified = action.call(id="elite-dangerous/flight-status", inputs={})
    return {"state": classified["flightStatus"]["state"], "text": classified["source"]["text"]}

def observe_sphere():
    return action.call(id="elite-dangerous/supercruise-sphere-direction", inputs={})

def positive_clear_status(state):
    return state in ["SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "FSD_ALIGNMENT_REQUIRED", "SAFE_DISENGAGE_READY"]

def expected_status(state):
    return state in ["UNKNOWN", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "FSD_ALIGNMENT_REQUIRED", "SAFE_DISENGAGE_READY"]

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

def emit_update(phase, sample, target_name, flight=None, observation=None, control=None, direction_confirmations=0, turn_pulses=0, separation_samples=0, clear_confirmations=0, commanded_throttle=None, last_command=None, reason=None):
    sphere = None if observation == None else observation["sphere"]
    direction = None if observation == None else observation["direction"]
    stream.emit(type="action.clear-supercruise-assist-line-of-sight.update", payload={
        "phase": phase, "sample": sample, "targetName": target_name,
        "flightStatus": None if flight == None else flight["state"],
        "flightPromptText": None if flight == None else flight["text"],
        "sphereState": None if sphere == None else sphere["state"],
        "sphereCenterX": None if sphere == None else sphere["centerX"],
        "sphereCenterY": None if sphere == None else sphere["centerY"],
        "sphereRadiusPixels": None if sphere == None else sphere["radiusPixels"],
        "signedLimbClearancePixels": None if sphere == None else sphere["signedLimbClearancePixels"],
        "sphereConfidencePermille": None if sphere == None else sphere["confidencePermille"],
        "directionState": None if direction == None else direction["state"],
        "control": control,
        "directionConfirmations": direction_confirmations,
        "turnPulses": turn_pulses,
        "fixedTurnElapsedMs": turn_pulses * TURN_PULSE_MS,
        "separationSamples": separation_samples,
        "clearConfirmations": clear_confirmations,
        "commandedThrottle": commanded_throttle,
        "lastCommand": last_command,
        "reason": reason,
    })

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
        elif expected_status(last_flight["state"]):
            gate_confirmations = 0
        else:
            fail("unexpected known flight status while confirming line-of-sight Gate: " + last_flight["state"])
        emit_update("CONFIRMING_GATE", sample, target_name, flight=last_flight, reason="LINE_OF_SIGHT_GATE_" + str(gate_confirmations) + "_OF_" + str(GATE_CONFIRMATIONS))
        if gate_confirmations >= GATE_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if gate_confirmations < GATE_CONFIRMATIONS:
        fail("LINE_OF_SIGHT_GATE_NOT_STABLE: prompt was not confirmed twice")

    stopped = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    install_failure_compensation()
    emit_update("STOPPING_DIRECT_APPROACH", sample, target_name, flight=last_flight, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="LINE_OF_SIGHT_GATE_CONFIRMED:" + stopped["control"])

    control = None
    direction_confirmations = 0
    for _ in range(DIRECTION_CONFIRMATIONS):
        sample += 1
        observation = observe_sphere()
        sphere = observation["sphere"]
        direction = observation["direction"]
        if sphere["state"] != "DETECTED" or direction["state"] != "READY":
            emit_update("CONFIRMING_OUTWARD_DIRECTION", sample, target_name, observation=observation, control=control, direction_confirmations=direction_confirmations, commanded_throttle=0, reason="DIRECTION_EVIDENCE_NOT_READY:" + direction["reason"])
            fail("LINE_OF_SIGHT_SPHERE_DIRECTION_UNKNOWN: " + direction["reason"])
        if control == None:
            control = direction["control"]
        elif direction["control"] != control:
            emit_update("CONFIRMING_OUTWARD_DIRECTION", sample, target_name, observation=observation, control=direction["control"], direction_confirmations=direction_confirmations, commanded_throttle=0, reason="DIRECTION_CONTROL_DISAGREEMENT:" + control + ":" + direction["control"])
            fail("LINE_OF_SIGHT_OUTWARD_DIRECTION_NOT_STABLE: fresh detections selected different controls")
        direction_confirmations += 1
        emit_update("CONFIRMING_OUTWARD_DIRECTION", sample, target_name, observation=observation, control=control, direction_confirmations=direction_confirmations, commanded_throttle=0, reason="COMPATIBLE_DIRECTION_" + str(direction_confirmations) + "_OF_" + str(DIRECTION_CONFIRMATIONS) + ":" + direction["reason"])
        if direction_confirmations < DIRECTION_CONFIRMATIONS:
            task.sleep(milliseconds=POLL_MS)

    turn_pulses = 0
    for _ in range(FIXED_TURN_PULSES):
        pulse(control)
        turn_pulses += 1
        sample += 1
        emit_update("EXECUTING_FIXED_OUTWARD_TURN", sample, target_name, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, commanded_throttle=0, last_command=control, reason="FIXED_OUTWARD_PULSE_" + str(turn_pulses) + "_OF_" + str(FIXED_TURN_PULSES))

    full = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    emit_update("SEPARATION_FLIGHT", sample, target_name, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, commanded_throttle=100, last_command="SET_THROTTLE_100", reason="FIXED_30_SECOND_OUTWARD_SEPARATION:" + full["control"])
    separation_samples = 0
    for _ in range(SEPARATION_SAMPLES):
        task.sleep(milliseconds=POLL_MS)
        separation_samples += 1
        sample += 1
        last_flight = observe_flight()
        if not expected_status(last_flight["state"]):
            fail("unexpected known flight status during outward separation: " + last_flight["state"])
        emit_update("SEPARATION_FLIGHT", sample, target_name, flight=last_flight, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, separation_samples=separation_samples, commanded_throttle=100, reason="OUTWARD_SEPARATION_" + str(separation_samples) + "_OF_" + str(SEPARATION_SAMPLES))

    stopped = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    clear_confirmations = 0
    for _ in range(CLEAR_OBSERVATION_LIMIT):
        sample += 1
        last_flight = observe_flight()
        if last_flight["state"] == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED" or last_flight["state"] == "UNKNOWN":
            clear_confirmations = 0
        elif positive_clear_status(last_flight["state"]):
            clear_confirmations += 1
        else:
            fail("unexpected known flight status after outward separation: " + last_flight["state"])
        emit_update("VERIFYING_PROMPT_CLEAR", sample, target_name, flight=last_flight, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, separation_samples=separation_samples, clear_confirmations=clear_confirmations, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="POSITIVE_PROMPT_CLEAR_" + str(clear_confirmations) + "_OF_" + str(CLEAR_CONFIRMATIONS))
        if clear_confirmations >= CLEAR_CONFIRMATIONS:
            action.clear_on_failure()
            emit_update("COMPLETED", sample, target_name, flight=last_flight, control=control, direction_confirmations=direction_confirmations, turn_pulses=turn_pulses, separation_samples=separation_samples, clear_confirmations=clear_confirmations, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="FIXED_TURN_AND_30_SECOND_SEPARATION_CONFIRMED:" + stopped["control"])
            stream.activity(message="Supercruise obstruction cleared after fixed outward turn and 30-second separation", level="info")
            return {"schemaVersion": 3, "task": "CLEAR_SUPERCRUISE_ASSIST_LINE_OF_SIGHT", "completed": True, "targetName": target_name, "control": control, "directionConfirmations": direction_confirmations, "turnPulses": turn_pulses, "fixedTurnDurationMs": FIXED_TURN_DURATION_MS, "fixedOutwardTurnCompleted": True, "separationDurationMs": SEPARATION_DURATION_MS, "separationSamples": separation_samples, "finalFlightStatus": last_flight["state"], "sampleCount": sample}
        task.sleep(milliseconds=POLL_MS)
    fail("LINE_OF_SIGHT_PROMPT_NOT_CLEAR_AFTER_SEPARATION: positive absence evidence was not confirmed")
