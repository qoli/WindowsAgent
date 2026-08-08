POLL_MS = 100
MAX_COMMANDS = 240
MAX_SAMPLES = 400
STABLE_CENTER_CONFIRMATIONS = 3
FINE_DISTANCE_PIXELS = 12

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

def emit_update(phase, sample, command_count, target, stable_confirmations, command=None, command_result=None, reason=None):
    stream.emit(
        type="action.align-station-target.update",
        payload={
            "phase": phase,
            "sample": sample,
            "commandCount": command_count,
            "targetDetected": target["detected"],
            "targetPresentation": target["presentation"],
            "targetHemisphere": target["hemisphere"],
            "offsetX": target["offsetX"],
            "offsetY": target["offsetY"],
            "centerDistancePixels": target["centerDistancePixels"],
            "centerZoneInside": target["centerZone"]["inside"],
            "stableConfirmations": stable_confirmations,
            "command": command,
            "commandResult": command_result,
            "reason": reason,
        },
    )

def choose_front_command(target):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    if abs(offset_x) >= abs(offset_y) and offset_x != 0:
        return "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
    if offset_y != 0:
        return "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
    return None

def choose_rear_command(target):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    if abs(offset_x) >= abs(offset_y) and offset_x != 0:
        return "YAW_LEFT" if offset_x > 0 else "YAW_RIGHT"
    if offset_y != 0:
        return "PITCH_UP" if offset_y > 0 else "PITCH_DOWN"
    return "YAW_RIGHT"

def main(ctx):
    stream.activity(message="Stopping ship before compass alignment", level="info")
    throttle_result = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("STOPPING", 0, 0, empty_target(), 0, command="SET_THROTTLE_0", command_result=throttle_result)

    sample = 0
    command_count = 0
    stable_confirmations = 0
    previous_phase = None
    final_observation = None

    for _ in range(MAX_SAMPLES):
        attempt = action.try_call(id="elite-dangerous/compass", inputs={})
        sample += 1
        if not attempt["ok"]:
            emit_update("OBSERVATION_ERROR", sample, command_count, empty_target(), stable_confirmations, reason=attempt["error"])
            fail("Compass observation failed: " + attempt["error"])

        observation = attempt["output"]
        target = observation["target"]
        final_observation = observation
        if not target["detected"]:
            emit_update("OBSERVING", sample, command_count, target, 0, reason="TARGET_NOT_DETECTED")
            fail("Compass target is not detected; establish the intended Station target lock first")
        if target["presentation"] == "UNKNOWN":
            emit_update("OBSERVING", sample, command_count, target, 0, reason="TARGET_PRESENTATION_UNKNOWN")
            fail("Compass target hollow or solid presentation is ambiguous")

        phase = "TURNING_TO_FRONT"
        command = None
        if target["presentation"] == "HOLLOW":
            stable_confirmations = 0
            command = choose_rear_command(target)
        elif target["centerZone"]["inside"]:
            stable_confirmations += 1
            phase = "VERIFYING_CENTER"
        else:
            stable_confirmations = 0
            if target["centerDistancePixels"] > FINE_DISTANCE_PIXELS:
                phase = "COARSE_ALIGN"
            else:
                phase = "FINE_ALIGN"
            command = choose_front_command(target)

        if phase != previous_phase:
            if phase == "TURNING_TO_FRONT":
                stream.activity(message="Turning rear target into front hemisphere", level="info")
            elif phase == "COARSE_ALIGN":
                stream.activity(message="Coarse compass alignment", level="info")
            elif phase == "FINE_ALIGN":
                stream.activity(message="Fine compass alignment", level="info")
            elif phase == "VERIFYING_CENTER":
                stream.activity(message="Verifying stable compass center", level="info")
            previous_phase = phase

        if stable_confirmations >= STABLE_CENTER_CONFIRMATIONS:
            emit_update("COMPLETED", sample, command_count, target, stable_confirmations, reason="SOLID_TARGET_STABLY_CENTERED")
            stream.activity(message="Station target aligned", level="info")
            return {
                "schemaVersion": 1,
                "task": "ALIGN_STATION_TARGET",
                "completed": True,
                "finalPhase": "COMPLETED",
                "sampleCount": sample,
                "commandCount": command_count,
                "stableConfirmations": stable_confirmations,
                "finalObservation": final_observation,
            }

        if command == None:
            emit_update(phase, sample, command_count, target, stable_confirmations, reason="WAITING_FOR_STABLE_CENTER")
            task.sleep(milliseconds=POLL_MS)
            continue
        if command_count >= MAX_COMMANDS:
            emit_update(phase, sample, command_count, target, stable_confirmations, reason="COMMAND_LIMIT_REACHED")
            fail("Compass alignment exhausted the bounded command limit")

        command_result = action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": command})
        command_count += 1
        stream.activity(message=command + " at " + str(target["centerDistancePixels"]) + " px", level="info")
        emit_update(phase, sample, command_count, target, stable_confirmations, command=command, command_result=command_result)
        task.sleep(milliseconds=POLL_MS)

    fail("Compass alignment exhausted the bounded sample limit")
