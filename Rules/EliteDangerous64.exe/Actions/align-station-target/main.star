POLL_MS = 100
MAX_COMMANDS = 120
MAX_SAMPLES = 240
STABLE_CENTER_CONFIRMATIONS = 3
FINE_DISTANCE_PIXELS = 12
ROLL_AXIS_TOLERANCE_PIXELS = 4
REAR_HOLD_MS = 800
COARSE_HOLD_MS = 800
FINE_HOLD_MS = 400
MIN_OBSERVED_MOVEMENT_PIXELS = 1
NO_MOVEMENT_LIMIT = 4
AWAY_TREND_LIMIT = 5

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

def emit_update(phase, sample, command_count, target, stable_confirmations, command=None, command_result=None, reason=None, command_hold_ms=None, observed_movement_pixels=None, no_movement_count=0, distance_delta_pixels=None, away_trend_count=0):
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
            "commandHoldMs": command_hold_ms,
            "commandResult": command_result,
            "observedMovementPixels": observed_movement_pixels,
            "noMovementCount": no_movement_count,
            "distanceDeltaPixels": distance_delta_pixels,
            "awayTrendCount": away_trend_count,
            "reason": reason,
        },
    )

def choose_front_command(target):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    if target["centerDistancePixels"] > FINE_DISTANCE_PIXELS and abs(offset_x) > ROLL_AXIS_TOLERANCE_PIXELS:
        return ["ROLL_RIGHT" if offset_x > 0 else "ROLL_LEFT", COARSE_HOLD_MS]
    hold_ms = FINE_HOLD_MS if target["centerDistancePixels"] <= FINE_DISTANCE_PIXELS else COARSE_HOLD_MS
    if abs(offset_x) >= abs(offset_y) and offset_x != 0:
        return ["YAW_RIGHT" if offset_x > 0 else "YAW_LEFT", hold_ms]
    if offset_y != 0:
        # Screen Y grows downward. Pitch up moves the front marker downward,
        # so a marker above center needs pitch up and one below needs pitch down.
        return ["PITCH_DOWN" if offset_y > 0 else "PITCH_UP", hold_ms]
    return None

def choose_rear_command(target):
    # A hollow marker is a rear-hemisphere projection. Its offset crosses the
    # compass center while the ship turns, so steering from the offset sign
    # reverses the command and oscillates around the antipode. Keep one strong
    # axis until the marker becomes solid; only then steer from screen offset.
    return ["PITCH_UP", REAR_HOLD_MS]

def main(ctx):
    stream.activity(message="Stopping ship before compass alignment", level="info")
    throttle_result = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("STOPPING", 0, 0, empty_target(), 0, command="SET_THROTTLE_0", command_result=throttle_result)

    sample = 0
    command_count = 0
    stable_confirmations = 0
    previous_phase = None
    final_observation = None
    commanded_target = None
    no_movement_count = 0
    away_trend_count = 0

    for _ in range(MAX_SAMPLES):
        attempt = action.try_call(id="elite-dangerous/compass", inputs={})
        sample += 1
        if not attempt["ok"]:
            emit_update("OBSERVATION_ERROR", sample, command_count, empty_target(), stable_confirmations, reason=attempt["error"])
            fail("Compass observation failed: " + attempt["error"])

        observation = attempt["output"]
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
            else:
                no_movement_count = 0
            distance_delta = target["centerDistancePixels"] - commanded_target["centerDistancePixels"]
            if commanded_target["presentation"] == "SOLID" and target["presentation"] == "SOLID":
                if distance_delta >= 1:
                    away_trend_count += 1
                else:
                    away_trend_count = 0
            else:
                away_trend_count = 0
        commanded_target = None
        if not target["detected"]:
            emit_update("OBSERVING", sample, command_count, target, 0, reason="TARGET_NOT_DETECTED")
            fail("Compass target is not detected; establish the intended Station target lock first")
        if target["presentation"] == "UNKNOWN":
            emit_update("OBSERVING", sample, command_count, target, 0, reason="TARGET_PRESENTATION_UNKNOWN")
            fail("Compass target hollow or solid presentation is ambiguous")
        if no_movement_count >= NO_MOVEMENT_LIMIT:
            emit_update("OBSERVING", sample, command_count, target, 0, reason="ATTITUDE_CONTROL_NO_PROGRESS", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            fail("Ship attitude control produced no measurable Compass movement")
        if away_trend_count >= AWAY_TREND_LIMIT:
            emit_update("OBSERVING", sample, command_count, target, 0, reason="ATTITUDE_CONTROL_MOVING_AWAY", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            fail("Ship attitude control moved the front Compass target away from center")

        phase = "TURNING_TO_FRONT"
        command_spec = None
        if target["presentation"] == "HOLLOW":
            stable_confirmations = 0
            command_spec = choose_rear_command(target)
        elif target["centerZone"]["inside"]:
            stable_confirmations += 1
            phase = "VERIFYING_CENTER"
        else:
            stable_confirmations = 0
            if target["centerDistancePixels"] > FINE_DISTANCE_PIXELS:
                phase = "COARSE_ALIGN"
            else:
                phase = "FINE_ALIGN"
            command_spec = choose_front_command(target)

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

        if command_spec == None:
            emit_update(phase, sample, command_count, target, stable_confirmations, reason="WAITING_FOR_STABLE_CENTER", observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
            task.sleep(milliseconds=POLL_MS)
            continue
        if command_count >= MAX_COMMANDS:
            emit_update(phase, sample, command_count, target, stable_confirmations, reason="COMMAND_LIMIT_REACHED")
            fail("Compass alignment exhausted the bounded command limit")

        command = command_spec[0]
        hold_ms = command_spec[1]
        command_result = action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": command, "holdMs": hold_ms})
        command_count += 1
        commanded_target = target
        stream.activity(message=command + " for " + str(hold_ms) + " ms at " + str(target["centerDistancePixels"]) + " px", level="info")
        emit_update(phase, sample, command_count, target, stable_confirmations, command=command, command_result=command_result, command_hold_ms=hold_ms, observed_movement_pixels=observed_movement, no_movement_count=no_movement_count, distance_delta_pixels=distance_delta, away_trend_count=away_trend_count)
        task.sleep(milliseconds=POLL_MS)

    fail("Compass alignment exhausted the bounded sample limit")
