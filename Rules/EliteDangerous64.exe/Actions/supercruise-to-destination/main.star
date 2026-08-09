POLL_MS = 500
INITIAL_ALIGN_LIMIT = 120
ENTER_LIMIT = 120
APPROACH_LIMIT = 2400
EXIT_LIMIT = 120
CENTER_CONFIRMATIONS = 3
SAFE_DISENGAGE_CONFIRMATIONS = 2
STOPPED_CONFIRMATIONS = 3
APPROACH_CENTER_PIXELS = 16
COARSE_DISTANCE_PIXELS = 40
COARSE_HOLD_MS = 800
MEDIUM_HOLD_MS = 300
FINE_HOLD_MS = 250

def emit_update(phase, sample, target_name, flight_status="UNKNOWN", prompt_text=None, mass_lock=None, landing_gear=None, cargo_scoop=None, target=None, safe_confirmations=0, stopped_confirmations=0, commanded_throttle=None, last_command=None, reason=None):
    stream.emit(
        type="action.supercruise-to-destination.update",
        payload={
            "phase": phase,
            "sample": sample,
            "targetName": target_name,
            "flightStatus": flight_status,
            "flightPromptText": prompt_text,
            "massLock": mass_lock,
            "landingGear": landing_gear,
            "cargoScoop": cargo_scoop,
            "targetDetected": None if target == None else target["detected"],
            "targetPresentation": None if target == None else target["presentation"],
            "targetOffsetX": None if target == None else target["offsetX"],
            "targetOffsetY": None if target == None else target["offsetY"],
            "targetCenterDistancePixels": None if target == None else target["centerDistancePixels"],
            "targetCenterZoneInside": None if target == None else target["centerZone"]["inside"],
            "safeDisengageConfirmations": safe_confirmations,
            "stoppedConfirmations": stopped_confirmations,
            "commandedThrottle": commanded_throttle,
            "lastCommand": last_command,
            "reason": reason,
        },
    )

def observe_flight():
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    classified = action.call(id="elite-dangerous/flight-status", inputs=raw)
    return {
        "state": classified["flightStatus"]["state"],
        "text": raw["text"],
    }

def observe_compass_target():
    observation = action.call(id="elite-dangerous/compass", inputs={})
    target = observation["target"]
    if not target["detected"]:
        fail("Compass target is absent; targetLocked=true did not match current visual evidence")
    if target["presentation"] == "UNKNOWN":
        fail("Compass target presentation is UNKNOWN")
    return target

def choose_alignment_command(target):
    if target["presentation"] == "HOLLOW":
        return ["YAW_LEFT", COARSE_HOLD_MS]
    distance = target["centerDistancePixels"]
    if distance <= APPROACH_CENTER_PIXELS:
        return None
    hold_ms = COARSE_HOLD_MS
    if distance <= APPROACH_CENTER_PIXELS * 2:
        hold_ms = FINE_HOLD_MS
    elif distance <= COARSE_DISTANCE_PIXELS:
        hold_ms = MEDIUM_HOLD_MS
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    if abs(offset_x) >= abs(offset_y) and offset_x != 0:
        return ["YAW_RIGHT" if offset_x > 0 else "YAW_LEFT", COARSE_HOLD_MS]
    if offset_y != 0:
        return ["PITCH_DOWN" if offset_y > 0 else "PITCH_UP", hold_ms]
    return None

def apply_alignment(target):
    command = choose_alignment_command(target)
    if command == None:
        return None
    action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": command[0], "holdMs": command[1]})
    return command[0] + ":" + str(command[1])

def preflight(target_name):
    ship = action.call(id="elite-dangerous/ship-status", inputs={})["shipStatus"]
    mass_lock = ship["massLock"]["state"]
    landing_gear = ship["landingGear"]["state"]
    cargo_scoop = ship["cargoScoop"]["state"]
    emit_update("PREFLIGHT", 0, target_name, mass_lock=mass_lock, landing_gear=landing_gear, cargo_scoop=cargo_scoop, reason="SHIP_STATUS_OBSERVED")
    if mass_lock != "OFF":
        fail("Supercruise requires visually confirmed Mass Lock OFF; observed " + mass_lock)
    if landing_gear != "OFF":
        fail("Supercruise requires visually confirmed Landing Gear OFF; observed " + landing_gear)
    if cargo_scoop != "OFF":
        fail("Supercruise requires visually confirmed Cargo Scoop OFF; observed " + cargo_scoop)

def align_initial(target_name):
    stable = 0
    for sample in range(1, INITIAL_ALIGN_LIMIT + 1):
        target = observe_compass_target()
        command = apply_alignment(target)
        if target["presentation"] == "SOLID" and target["centerDistancePixels"] <= APPROACH_CENTER_PIXELS:
            stable += 1
        else:
            stable = 0
        emit_update("ALIGNING", sample, target_name, target=target, last_command=command, reason="INITIAL_TARGET_ALIGNMENT")
        if stable >= CENTER_CONFIRMATIONS:
            return sample
        task.sleep(milliseconds=POLL_MS)
    fail("target did not reach three consecutive aligned Compass observations")

def main(ctx):
    target_name = ctx.inputs["targetName"]
    if not ctx.inputs["targetLocked"]:
        fail("targetLocked must be true after select-and-lock-destination completes")
    if not ctx.inputs["normalSpaceConfirmed"]:
        fail("normalSpaceConfirmed must be true before the Supercruise toggle is allowed")

    stream.activity(message="Checking Supercruise preflight Gates", level="info")
    preflight(target_name)
    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("PREFLIGHT", 0, target_name, commanded_throttle=0, last_command="SET_THROTTLE_0", reason=throttle["control"])

    stream.activity(message="Aligning selected destination", level="info")
    sample = align_initial(target_name)

    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    fsd = action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    stream.activity(message="Entering Supercruise", level="info")
    emit_update("CHARGING", sample, target_name, commanded_throttle=100, last_command="SUPERCRUISE_TOGGLE", reason=fsd["control"] + "+" + throttle["control"])

    entered = False
    charging_seen = False
    last_flight_status = "UNKNOWN"
    last_prompt_text = None
    for _ in range(ENTER_LIMIT):
        sample += 1
        flight = observe_flight()
        last_flight_status = flight["state"]
        last_prompt_text = flight["text"]
        target = None
        command = None
        phase = "CHARGING"
        if last_flight_status == "FSD_CHARGING":
            charging_seen = True
        elif last_flight_status == "FSD_ALIGNMENT_REQUIRED":
            charging_seen = True
            phase = "ALIGNING_FOR_ENTRY"
            target = observe_compass_target()
            command = apply_alignment(target)
        elif last_flight_status == "SUPERCRUISE" or last_flight_status == "SAFE_DISENGAGE_READY":
            if charging_seen:
                entered = True
                phase = "ENTERED"
            else:
                fail("SUPERCRUISE appeared without a preceding FSD charging observation")
        elif last_flight_status not in ["FSD_CHARGING", "UNKNOWN"]:
            fail("unexpected known flight status while entering Supercruise: " + last_flight_status)
        emit_update(phase, sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, target=target, commanded_throttle=100, last_command=command, reason="WAITING_FOR_SUPERCRUISE_VISUAL_CONFIRMATION")
        if entered:
            break
        task.sleep(milliseconds=POLL_MS)
    if not entered:
        fail("FSD charging followed by SUPERCRUISE was not visually confirmed before the entry limit")

    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 75})
    stream.activity(message="Supercruise approach at 75% throttle", level="info")
    emit_update("APPROACHING", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, commanded_throttle=75, last_command="SET_THROTTLE_75", reason=throttle["control"])

    safe_confirmations = 0
    for _ in range(APPROACH_LIMIT):
        sample += 1
        flight = observe_flight()
        last_flight_status = flight["state"]
        last_prompt_text = flight["text"]
        target = observe_compass_target()
        command = apply_alignment(target)
        if last_flight_status == "SAFE_DISENGAGE_READY":
            safe_confirmations += 1
        else:
            safe_confirmations = 0
        phase = "SAFE_DISENGAGE_READY" if safe_confirmations > 0 else "APPROACHING"
        emit_update(phase, sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, target=target, safe_confirmations=safe_confirmations, commanded_throttle=75, last_command=command, reason="WAITING_FOR_TWO_SAFE_DISENGAGE_FRAMES")
        if safe_confirmations >= SAFE_DISENGAGE_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if safe_confirmations < SAFE_DISENGAGE_CONFIRMATIONS:
        fail("SAFE DISENGAGE READY was not confirmed before the approach limit")

    stream.activity(message="Safe disengage Gate confirmed", level="info")
    fsd = action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
    throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("DISENGAGING", sample, target_name, flight_status=last_flight_status, prompt_text=last_prompt_text, safe_confirmations=safe_confirmations, commanded_throttle=0, last_command="SUPERCRUISE_TOGGLE", reason=fsd["control"] + "+" + throttle["control"])

    stopped_confirmations = 0
    final_speed = None
    for _ in range(EXIT_LIMIT):
        sample += 1
        speed = action.call(id="elite-dangerous/ship-speed", inputs={})["speed"]
        final_speed = speed
        if speed["state"] == "STOPPED":
            stopped_confirmations += 1
        else:
            stopped_confirmations = 0
        emit_update("VERIFYING_STOP", sample, target_name, safe_confirmations=safe_confirmations, stopped_confirmations=stopped_confirmations, commanded_throttle=0, reason="SHIP_SPEED_" + speed["state"])
        if stopped_confirmations >= STOPPED_CONFIRMATIONS:
            action.clear_on_failure()
            stream.activity(message="Nav destination reached in normal space", level="info")
            emit_update("COMPLETED", sample, target_name, safe_confirmations=safe_confirmations, stopped_confirmations=stopped_confirmations, commanded_throttle=0, reason="SAFE_DISENGAGE_AND_STOP_CONFIRMED")
            return {
                "schemaVersion": 1,
                "task": "SUPERCRUISE_TO_DESTINATION",
                "completed": True,
                "finalPhase": "COMPLETED",
                "targetName": target_name,
                "safeDisengageConfirmations": safe_confirmations,
                "stoppedConfirmations": stopped_confirmations,
                "finalCommandedThrottle": 0,
                "finalSpeed": final_speed,
                "sampleCount": sample,
            }
        task.sleep(milliseconds=POLL_MS)
    fail("normal-space STOPPED state was not visually confirmed after safe disengage")
