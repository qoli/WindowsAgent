POLL_MS = 500
GATE_CONFIRMATIONS = 2
GATE_OBSERVATION_LIMIT = 6
CLEAR_CONFIRMATIONS = 2
CLEAR_OBSERVATION_LIMIT = 8

def observe_flight():
    classified = action.call(id="elite-dangerous/flight-status", inputs={})
    return {"state": classified["flightStatus"]["state"], "text": classified["source"]["text"]}

def positive_clear_status(state):
    return state in ["SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "FSD_ALIGNMENT_REQUIRED", "SAFE_DISENGAGE_READY"]

def expected_status(state):
    return state in ["UNKNOWN", "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED", "SUPERCRUISE", "SUPERCRUISE_ASSIST_ACTIVE", "FSD_ALIGNMENT_REQUIRED", "SAFE_DISENGAGE_READY"]

def install_failure_compensation():
    action.clear_on_failure()
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)

def emit_update(phase, sample, target_name, flight=None, core=None, clear_confirmations=0, reason=None):
    stream.emit(type="action.clear-supercruise-assist-line-of-sight.update", payload={
        "phase": phase,
        "sample": sample,
        "targetName": target_name,
        "flightStatus": None if flight == None else flight["state"],
        "flightPromptText": None if flight == None else flight["text"],
        "coreCompleted": None if core == None else core["completed"],
        "control": None if core == None else core["control"],
        "directionConfirmations": 0 if core == None else core["directionConfirmations"],
        "turnPulses": 0 if core == None else core["turnPulses"],
        "fixedTurnElapsedMs": 0 if core == None else core["fixedTurnDurationMs"],
        "separationSamples": 0 if core == None else core["separationSamples"],
        "clearConfirmations": clear_confirmations,
        "commandedThrottle": None if core == None else core["finalCommandedThrottle"],
        "lastCommand": None if core == None else "FIXED_SUPERCRUISE_SPHERE_SEPARATION",
        "reason": reason,
    })

def main(ctx):
    target_name = ctx.inputs["targetName"]
    sample = 0
    gate_confirmations = 0
    last_flight = None

    install_failure_compensation()
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

    emit_update("CLEARING_CENTERED_SPHERE", sample, target_name, flight=last_flight, reason="LINE_OF_SIGHT_GATE_CONFIRMED:DELEGATING_FIXED_CLEARANCE")
    core = action.call(id="elite-dangerous/fixed-supercruise-sphere-separation", inputs={})
    if not core["completed"] or core["directionConfirmations"] != 2 or core["turnPulses"] != 8 or core["fixedTurnDurationMs"] != 6400 or core["separationDurationMs"] != 30000 or core["separationSamples"] != 60 or not core["finalSupercruiseConfirmed"] or core["finalCommandedThrottle"] != 0:
        fail("centered Supercruise sphere core returned an incomplete fixed-clearance postcondition")
    sample += core["sampleCount"]
    emit_update("CLEARING_CENTERED_SPHERE", sample, target_name, core=core, reason="FIXED_TURN_AND_SEPARATION_COMPLETED")

    clear_confirmations = 0
    for _ in range(CLEAR_OBSERVATION_LIMIT):
        sample += 1
        last_flight = observe_flight()
        if last_flight["state"] == "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED" or last_flight["state"] == "UNKNOWN":
            clear_confirmations = 0
        elif positive_clear_status(last_flight["state"]):
            clear_confirmations += 1
        else:
            fail("unexpected known flight status after centered-sphere clearance: " + last_flight["state"])
        emit_update("VERIFYING_PROMPT_CLEAR", sample, target_name, flight=last_flight, core=core, clear_confirmations=clear_confirmations, reason="POSITIVE_PROMPT_CLEAR_" + str(clear_confirmations) + "_OF_" + str(CLEAR_CONFIRMATIONS))
        if clear_confirmations >= CLEAR_CONFIRMATIONS:
            action.clear_on_failure()
            emit_update("COMPLETED", sample, target_name, flight=last_flight, core=core, clear_confirmations=clear_confirmations, reason="SHARED_FIXED_CLEARANCE_AND_POSITIVE_PROMPT_CLEAR_CONFIRMED")
            stream.activity(message="Supercruise Assist line-of-sight obstruction cleared by shared fixed sphere clearance", level="info")
            return {
                "schemaVersion": 4,
                "task": "CLEAR_SUPERCRUISE_ASSIST_LINE_OF_SIGHT",
                "completed": True,
                "targetName": target_name,
                "control": core["control"],
                "directionConfirmations": core["directionConfirmations"],
                "turnPulses": core["turnPulses"],
                "fixedTurnDurationMs": core["fixedTurnDurationMs"],
                "fixedOutwardTurnCompleted": True,
                "separationDurationMs": core["separationDurationMs"],
                "separationSamples": core["separationSamples"],
                "finalSupercruiseConfirmed": core["finalSupercruiseConfirmed"],
                "finalCommandedThrottle": core["finalCommandedThrottle"],
                "finalFlightStatus": last_flight["state"],
                "sampleCount": sample,
            }
        task.sleep(milliseconds=POLL_MS)
    fail("LINE_OF_SIGHT_PROMPT_NOT_CLEAR_AFTER_SEPARATION: positive absence evidence was not confirmed")
