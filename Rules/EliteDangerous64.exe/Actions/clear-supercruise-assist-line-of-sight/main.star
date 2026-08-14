POLL_MS = 500
GATE_CONFIRMATIONS = 2
GATE_OBSERVATION_LIMIT = 6
TURN_PULSE_MS = 800
MAX_TURN_PULSES = 40
MAX_NO_PROGRESS_PULSES = 5
MIN_CLEARANCE_PROGRESS_PIXELS = 8.0
EDGE_MARGIN_PIXELS = 12.0
ABSENT_CONFIRMATIONS = 3
ABSENT_BEFORE_EDGE_LIMIT = 2
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

def sphere_edge_reached(sphere):
    if sphere["state"] != "DETECTED":
        return False
    x = sphere["centerX"]
    y = sphere["centerY"]
    radius = sphere["radiusPixels"]
    return x - radius <= EDGE_MARGIN_PIXELS or x + radius >= 1920.0 - EDGE_MARGIN_PIXELS or y - radius <= EDGE_MARGIN_PIXELS or y + radius >= 1080.0 - EDGE_MARGIN_PIXELS

def emit_update(phase, sample, target_name, flight=None, observation=None, control=None, turn_pulses=0, no_progress=0, absent_confirmations=0, separation_samples=0, clear_confirmations=0, commanded_throttle=None, last_command=None, reason=None):
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
        "turnPulses": turn_pulses, "noProgressPulses": no_progress,
        "absentConfirmations": absent_confirmations, "separationSamples": separation_samples,
        "clearConfirmations": clear_confirmations, "commandedThrottle": commanded_throttle,
        "lastCommand": last_command, "reason": reason,
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

    observation = observe_sphere()
    direction = observation["direction"]
    emit_update("SELECTING_OUTWARD_DIRECTION", sample, target_name, flight=last_flight, observation=observation, control=direction["control"], commanded_throttle=0, reason=direction["reason"])
    if observation["sphere"]["state"] != "DETECTED" or direction["state"] != "READY":
        fail("LINE_OF_SIGHT_SPHERE_DIRECTION_UNKNOWN: " + direction["reason"])

    control = direction["control"]
    previous_clearance = observation["sphere"]["signedLimbClearancePixels"]
    edge_reached = sphere_edge_reached(observation["sphere"])
    no_progress = 0
    absent_confirmations = 0
    absent_before_edge = 0
    turn_pulses = 0
    for _ in range(MAX_TURN_PULSES):
        turn_pulses += 1
        pulse(control)
        sample += 1
        observation = observe_sphere()
        sphere = observation["sphere"]
        if sphere["state"] == "DETECTED":
            absent_confirmations = 0
            absent_before_edge = 0
            current_clearance = sphere["signedLimbClearancePixels"]
            progress = current_clearance - previous_clearance
            no_progress = no_progress + 1 if progress < MIN_CLEARANCE_PROGRESS_PIXELS else 0
            previous_clearance = current_clearance
            edge_reached = edge_reached or sphere_edge_reached(sphere)
            emit_update("TURNING_OUTWARD", sample, target_name, observation=observation, control=control, turn_pulses=turn_pulses, no_progress=no_progress, commanded_throttle=0, last_command=control, reason="FIXED_OUTWARD_DIRECTION:EDGE_REACHED=" + str(edge_reached))
            if no_progress >= MAX_NO_PROGRESS_PULSES:
                fail("LINE_OF_SIGHT_OUTWARD_TURN_NO_PROGRESS: sphere clearance did not increase")
        elif sphere["state"] == "ABSENT":
            if not edge_reached:
                absent_before_edge += 1
                emit_update("VERIFYING_SPHERE_EXIT", sample, target_name, observation=observation, control=control, turn_pulses=turn_pulses, commanded_throttle=0, last_command=control, reason="SPHERE_ABSENT_BEFORE_OBSERVED_EDGE_" + str(absent_before_edge) + "_OF_" + str(ABSENT_BEFORE_EDGE_LIMIT))
                if absent_before_edge >= ABSENT_BEFORE_EDGE_LIMIT:
                    fail("LINE_OF_SIGHT_SPHERE_LOST_BEFORE_EDGE: detector absence cannot prove exit")
            else:
                absent_confirmations += 1
                emit_update("VERIFYING_SPHERE_EXIT", sample, target_name, observation=observation, control=control, turn_pulses=turn_pulses, absent_confirmations=absent_confirmations, commanded_throttle=0, last_command=control, reason="SPHERE_EXIT_" + str(absent_confirmations) + "_OF_" + str(ABSENT_CONFIRMATIONS))
                if absent_confirmations >= ABSENT_CONFIRMATIONS:
                    break
        else:
            fail("LINE_OF_SIGHT_SPHERE_UNKNOWN_DURING_TURN: current-frame geometry is ambiguous")
        task.sleep(milliseconds=POLL_MS)
    if absent_confirmations < ABSENT_CONFIRMATIONS:
        fail("LINE_OF_SIGHT_SPHERE_EXIT_LIMIT: body did not leave the viewport")

    full = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    emit_update("SEPARATION_FLIGHT", sample, target_name, observation=observation, control=control, turn_pulses=turn_pulses, absent_confirmations=absent_confirmations, commanded_throttle=100, last_command="SET_THROTTLE_100", reason="FIXED_30_SECOND_OUTWARD_SEPARATION:" + full["control"])
    separation_samples = 0
    for _ in range(SEPARATION_SAMPLES):
        task.sleep(milliseconds=POLL_MS)
        separation_samples += 1
        sample += 1
        last_flight = observe_flight()
        if not expected_status(last_flight["state"]):
            fail("unexpected known flight status during outward separation: " + last_flight["state"])
        emit_update("SEPARATION_FLIGHT", sample, target_name, flight=last_flight, control=control, turn_pulses=turn_pulses, absent_confirmations=absent_confirmations, separation_samples=separation_samples, commanded_throttle=100, reason="OUTWARD_SEPARATION_" + str(separation_samples) + "_OF_" + str(SEPARATION_SAMPLES))

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
        emit_update("VERIFYING_PROMPT_CLEAR", sample, target_name, flight=last_flight, control=control, turn_pulses=turn_pulses, absent_confirmations=absent_confirmations, separation_samples=separation_samples, clear_confirmations=clear_confirmations, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="POSITIVE_PROMPT_CLEAR_" + str(clear_confirmations) + "_OF_" + str(CLEAR_CONFIRMATIONS))
        if clear_confirmations >= CLEAR_CONFIRMATIONS:
            action.clear_on_failure()
            emit_update("COMPLETED", sample, target_name, flight=last_flight, control=control, turn_pulses=turn_pulses, absent_confirmations=absent_confirmations, separation_samples=separation_samples, clear_confirmations=clear_confirmations, commanded_throttle=0, last_command="SET_THROTTLE_0", reason="SPHERE_EXIT_AND_30_SECOND_SEPARATION_CONFIRMED:" + stopped["control"])
            stream.activity(message="Supercruise obstruction body cleared with 30-second separation", level="info")
            return {"schemaVersion": 2, "task": "CLEAR_SUPERCRUISE_ASSIST_LINE_OF_SIGHT", "completed": True, "targetName": target_name, "control": control, "turnPulses": turn_pulses, "sphereExitConfirmed": True, "separationDurationMs": SEPARATION_DURATION_MS, "separationSamples": separation_samples, "finalFlightStatus": last_flight["state"], "sampleCount": sample}
        task.sleep(milliseconds=POLL_MS)
    fail("LINE_OF_SIGHT_PROMPT_NOT_CLEAR_AFTER_SEPARATION: positive absence evidence was not confirmed")
