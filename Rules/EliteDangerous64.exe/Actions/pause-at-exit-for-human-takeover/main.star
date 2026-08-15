DOWN_STEPS = 5
PAUSE_SETTLE_MS = 750
NAVIGATION_STEP_MS = 120
EXIT_DESTINATION_SETTLE_MS = 750
POST_SEQUENCE_SETTLE_MS = 1000

def emit(phase, step, last_command, reason):
    stream.emit(type="action.pause-at-exit-for-human-takeover.update", payload={
        "phase": phase,
        "step": step,
        "lastCommand": last_command,
        "reason": reason,
    })

def send(control, phase, step, reason, settle_ms):
    action.call(id="elite-dangerous/ui-control", inputs={"control": control})
    emit(phase, step, control, reason)
    if settle_ms > 0:
        task.sleep(milliseconds=settle_ms)

def main(ctx):
    step = 1
    send("PAUSE", "REPLAYING_PAUSE", step, "FIXED_SAFE_EXIT_PAUSE_SENT", PAUSE_SETTLE_MS)

    for navigation_step in range(DOWN_STEPS):
        step += 1
        send("DOWN", "REPLAYING_EXIT_NAVIGATION", step, "FIXED_SAFE_EXIT_DOWN_" + str(navigation_step + 1) + "_OF_" + str(DOWN_STEPS), NAVIGATION_STEP_MS)

    step += 1
    send("SELECT", "REPLAYING_FIRST_SELECT", step, "FIXED_SAFE_EXIT_FIRST_SELECT_SENT", EXIT_DESTINATION_SETTLE_MS)

    step += 1
    send("SELECT", "REPLAYING_SECOND_SELECT", step, "FIXED_SAFE_EXIT_SECOND_SELECT_SENT", POST_SEQUENCE_SETTLE_MS)

    stream.activity(message="Fixed safe-exit key replay completed for human takeover", level="warning")
    return {
        "schemaVersion": 3,
        "task": "PAUSE_AT_EXIT_FOR_HUMAN_TAKEOVER",
        "completed": True,
        "keyReplayCompleted": True,
        "pauseSent": True,
        "downCount": DOWN_STEPS,
        "firstSelectSent": True,
        "secondSelectSent": True,
        "sequenceLength": step,
        "visualPostconditionClaimed": False,
    }
