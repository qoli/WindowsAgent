POLL_MS = 250
APPROACH_POLL_MS = 1000
STATUS_RETRY_LIMIT = 3
SHIP_STATUS_CONFIRMATIONS = 2
PROBE_SAMPLE_LIMIT = 12
ESCAPE_VECTOR_CONFIRMATIONS_REQUIRED = 2
NON_ESCAPE_PROMPT_CONFIRMATIONS_REQUIRED = 2
ENTER_SUPERCRUISE_LIMIT = 120
DROP_CONFIRMATION_LIMIT = 40
APPROACH_SAMPLE_LIMIT = 240
APPROACH_UNKNOWN_LIMIT = 8
GRAVITY_CANDIDATE_CONFIRMATIONS_REQUIRED = 3
MAX_INCREASING_DISTANCE_SAMPLES = 3
GRAVITY_CANDIDATE_DISTANCE_METERS = 20000000.0
GRAVITY_CANDIDATE_SPEED_METERS_PER_SECOND = 20000.0
MAX_START_HEAT_PERCENT = 60
MIN_SPEED_DETECTION_CONFIDENCE = 0.60
MIN_SPEED_RECOGNITION_CONFIDENCE = 0.70

def emit_update(phase, target_name, sample, commanded_throttle=0, flight_status=None, status=None, heat_percent=None, target_distance=None, speed=None, trend="UNKNOWN", candidate_confirmations=0, escape_confirmations=0, reason=None):
    stream.emit(type="action.enter-planet-gravity-well.update", payload={
        "phase": phase,
        "targetName": target_name,
        "sample": sample,
        "commandedThrottle": commanded_throttle,
        "flightStatus": flight_status,
        "supercruise": None if status == None else status["supercruise"],
        "fsdCharging": None if status == None else status["fsdCharging"],
        "heatPercent": heat_percent,
        "targetDistanceMeters": target_distance,
        "supercruiseSpeedMetersPerSecond": speed,
        "distanceTrend": trend,
        "candidateConfirmations": candidate_confirmations,
        "escapeVectorConfirmations": escape_confirmations,
        "reason": reason,
    })

def has_flag(value, bit_value):
    return (value // bit_value) % 2 == 1

def observe_status():
    last_error = None
    for attempt_index in range(STATUS_RETRY_LIMIT):
        attempt = action.try_call(id="elite-dangerous/filesystem/status", inputs={})
        if attempt["ok"]:
            raw = attempt["output"]
            if raw["state"] != "AVAILABLE":
                fail("Status.json is required to prove the gravity-well lifecycle")
            data = raw["data"]
            if "Flags" not in data or "Flags2" not in data:
                fail("Status.json Flags and Flags2 are required")
            flags = data["Flags"]
            flags2 = data["Flags2"]
            destination = None
            if "Destination" in data and data["Destination"] != None and "Name" in data["Destination"]:
                destination = data["Destination"]["Name"]
            return {
                "supercruise": has_flag(flags, 16),
                "massLock": has_flag(flags, 65536),
                "fsdCharging": has_flag(flags, 131072),
                "fsdCooldown": has_flag(flags, 262144),
                "overHeating": has_flag(flags, 1048576),
                "fsdHyperdriveCharging": has_flag(flags2, 524288),
                "destination": destination,
                "sourceTimestamp": raw["source"]["sourceTimestamp"],
            }
        last_error = attempt["error"]
        if attempt_index + 1 < STATUS_RETRY_LIMIT:
            task.sleep(milliseconds=POLL_MS)
    fail("Status.json observer failed after bounded retries: " + last_error)

def observe_flight_status():
    classified_attempt = action.try_call(id="elite-dangerous/flight-status", inputs={})
    if not classified_attempt["ok"]:
        return {"state": "UNKNOWN", "reason": "FLIGHT_STATUS_FAILED: " + classified_attempt["error"]}
    return {"state": classified_attempt["output"]["flightStatus"]["state"], "reason": classified_attempt["output"]["source"]["text"]}

def set_safe_failure_compensation():
    action.clear_on_failure()
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)

def set_probe_failure_compensation():
    action.clear_on_failure()
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    action.on_failure(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"}, critical=True, timeout_milliseconds=2000)

def cancel_owned_charge(target_name, sample, flight_status, status, escape_confirmations, phase):
    # Remove the generic toggle compensation before issuing the one explicit
    # matching cancel. A later observer failure must never toggle charging on.
    set_safe_failure_compensation()
    action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    emit_update("CANCELLING_PROBE", target_name, sample, flight_status=flight_status, status=status, escape_confirmations=escape_confirmations, reason=phase + "_PROBE_CANCEL_SENT")
    for _ in range(DROP_CONFIRMATION_LIMIT):
        current = observe_status()
        if not current["fsdCharging"] and not current["fsdHyperdriveCharging"] and not current["supercruise"]:
            return current
        task.sleep(milliseconds=POLL_MS)
    fail("owned FSD probe did not return to normal-space idle after cancellation")

def probe_escape_vector(target_name, sample, phase):
    before = observe_status()
    if before["supercruise"] or before["fsdCharging"] or before["fsdHyperdriveCharging"]:
        fail("gravity-well probe requires normal-space idle")
    set_probe_failure_compensation()
    action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
    escape_confirmations = 0
    non_escape_confirmations = 0
    last_flight = "UNKNOWN"
    last_status = before
    for _ in range(PROBE_SAMPLE_LIMIT):
        sample += 1
        last_status = observe_status()
        flight = observe_flight_status()
        last_flight = flight["state"]
        if last_flight == "FSD_ESCAPE_VECTOR_REQUIRED":
            escape_confirmations += 1
        else:
            escape_confirmations = 0
        if last_flight in ["FSD_THROTTLE_UP_REQUIRED", "FSD_ALIGNMENT_REQUIRED"]:
            non_escape_confirmations += 1
        else:
            non_escape_confirmations = 0
        emit_update(phase, target_name, sample, flight_status=last_flight, status=last_status, escape_confirmations=escape_confirmations, reason=flight["reason"])
        if escape_confirmations >= ESCAPE_VECTOR_CONFIRMATIONS_REQUIRED:
            idle_status = cancel_owned_charge(target_name, sample, last_flight, last_status, escape_confirmations, phase)
            return {"detected": True, "sample": sample, "confirmations": escape_confirmations, "status": idle_status, "flightStatus": last_flight}
        if non_escape_confirmations >= NON_ESCAPE_PROMPT_CONFIRMATIONS_REQUIRED:
            idle_status = cancel_owned_charge(target_name, sample, last_flight, last_status, 0, phase)
            return {"detected": False, "sample": sample, "confirmations": 0, "status": idle_status, "flightStatus": last_flight}
        if last_status["supercruise"]:
            idle_status = cancel_owned_charge(target_name, sample, last_flight, last_status, escape_confirmations, phase)
            return {"detected": False, "sample": sample, "confirmations": 0, "status": idle_status, "flightStatus": last_flight}
        task.sleep(milliseconds=POLL_MS)
    idle_status = cancel_owned_charge(target_name, sample, last_flight, last_status, escape_confirmations, phase)
    return {"detected": False, "sample": sample, "confirmations": 0, "status": idle_status, "flightStatus": last_flight}

def is_digit(character):
    return character in "0123456789"

def first_decimal(text):
    numeric = ""
    started = False
    dot_seen = False
    for index in range(len(text)):
        character = text[index]
        if is_digit(character):
            numeric += character
            started = True
        elif character == "." and started and not dot_seen:
            numeric += character
            dot_seen = True
        elif started:
            break
    if len(numeric) == 0 or numeric == "." or numeric.endswith("."):
        return None
    parts = numeric.split(".")
    integer = 0
    for index in range(len(parts[0])):
        character = parts[0][index]
        integer = integer * 10 + "0123456789".find(character)
    if len(parts) == 1:
        return float(integer)
    fraction = 0
    denominator = 1
    for index in range(len(parts[1])):
        character = parts[1][index]
        fraction = fraction * 10 + "0123456789".find(character)
        denominator *= 10
    return float(integer) + float(fraction) / float(denominator)

def letters_only(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ":
            result += character
    return result

def observe_supercruise_speed():
    attempt = action.try_call(id="elite-dangerous/ship-speed-text-regions", inputs={})
    if not attempt["ok"]:
        return {"ok": False, "value": None, "reason": "SPEED_OCR_FAILED: " + attempt["error"]}
    best = None
    best_score = -1.0
    for region in attempt["output"]["regions"]:
        detection = region["detectionConfidence"]
        recognition = region["recognitionConfidence"]
        if detection < MIN_SPEED_DETECTION_CONFIDENCE or recognition < MIN_SPEED_RECOGNITION_CONFIDENCE:
            continue
        letters = letters_only(region["text"])
        multiplier = None
        if letters == "KMS":
            multiplier = 1000.0
        elif letters == "MMS":
            multiplier = 1000000.0
        elif letters == "C":
            multiplier = 299792458.0
        numeric = first_decimal(region["text"])
        if multiplier != None and numeric != None:
            score = detection * recognition
            if score > best_score:
                best_score = score
                best = numeric * multiplier
    if best == None:
        return {"ok": False, "value": None, "reason": "SUPERCRUISE_SPEED_TEXT_UNKNOWN"}
    return {"ok": True, "value": best, "reason": "SUPERCRUISE_SPEED_TEXT_ACCEPTED"}

def observe_target_distance():
    attempt = action.try_call(id="elite-dangerous/request-docking-range", inputs={})
    if not attempt["ok"]:
        return {"ok": False, "value": None, "reason": "TARGET_DISTANCE_OCR_FAILED: " + attempt["error"]}
    gate = attempt["output"]["requestDockingRange"]
    if gate["distanceMeters"] == None or gate["state"] == "UNKNOWN":
        return {"ok": False, "value": None, "reason": "TARGET_DISTANCE_UNKNOWN: " + gate["evidence"]["reason"]}
    return {"ok": True, "value": gate["distanceMeters"], "reason": gate["evidence"]["reason"]}

def preflight(target_name):
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    set_safe_failure_compensation()
    status = observe_status()
    if status["fsdCharging"] or status["fsdHyperdriveCharging"]:
        fail("enter-planet-gravity-well requires an idle FSD")
    if status["destination"] != target_name:
        fail("Status.json Destination does not match targetName: " + str(status["destination"]))
    if status["massLock"]:
        fail("enter-planet-gravity-well requires Status.json Mass Lock OFF")
    heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
    if heat["state"] != "KNOWN" or heat["percent"] > MAX_START_HEAT_PERCENT:
        fail("enter-planet-gravity-well requires visual heat at or below 60%")
    confirmations = 0
    for _ in range(SHIP_STATUS_CONFIRMATIONS * 2):
        ship = action.call(id="elite-dangerous/ship-status", inputs={})["shipStatus"]
        if ship["massLock"]["state"] == "OFF" and ship["landingGear"]["state"] == "OFF" and ship["cargoScoop"]["state"] == "OFF":
            confirmations += 1
        else:
            confirmations = 0
        if confirmations >= SHIP_STATUS_CONFIRMATIONS:
            reason = "SUPERCRUISE_TARGET_AND_SHIP_STATUS_CONFIRMED" if status["supercruise"] else "NORMAL_SPACE_TARGET_AND_SHIP_STATUS_CONFIRMED"
            emit_update("PREFLIGHT", target_name, 0, status=status, heat_percent=heat["percent"], reason=reason)
            return status, heat["percent"]
        task.sleep(milliseconds=POLL_MS)
    fail("visual ship-status preflight did not produce two consecutive all-OFF samples")

def completed_output(target_name, entry_mode, probe, approach_samples, target_distance, speed):
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    action.clear_on_failure()
    emit_update("COMPLETED", target_name, probe["sample"], status=probe["status"], flight_status=probe["flightStatus"], target_distance=target_distance, speed=speed, trend="CANDIDATE" if approach_samples > 0 else "UNKNOWN", candidate_confirmations=GRAVITY_CANDIDATE_CONFIRMATIONS_REQUIRED if approach_samples > 0 else 0, escape_confirmations=probe["confirmations"], reason=entry_mode)
    return {
        "schemaVersion": 1,
        "task": "ENTER_PLANET_GRAVITY_WELL",
        "completed": True,
        "finalPhase": "COMPLETED",
        "entryMode": entry_mode,
        "targetName": target_name,
        "escapeVectorConfirmations": probe["confirmations"],
        "approachSampleCount": approach_samples,
        "finalTargetDistanceMeters": target_distance,
        "finalSupercruiseSpeedMetersPerSecond": speed,
        "normalSpaceConfirmed": True,
        "finalCommandedThrottle": 0,
    }

def main(ctx):
    target_name = ctx.inputs["targetName"]
    status, heat_percent = preflight(target_name)
    sample = 0
    entry_mode = "SUPERCRUISE_HANDOFF_APPROACHED_AND_VERIFIED" if status["supercruise"] else "APPROACHED_AND_VERIFIED"
    if status["supercruise"]:
        stream.activity(message="Taking over the confirmed Supercruise approach", level="info")
        emit_update("APPROACHING", target_name, sample, status=status, heat_percent=heat_percent, reason="ALREADY_IN_SUPERCRUISE_APPROACH")
    else:
        stream.activity(message="Probing current position for Escape Vector evidence", level="info")
        initial_probe = probe_escape_vector(target_name, 0, "PROBING_CURRENT_POSITION")
        if initial_probe["detected"]:
            stream.activity(message="Gravity well was already present; owned FSD probe cancelled", level="info")
            return completed_output(target_name, "ALREADY_IN_GRAVITY_WELL", initial_probe, 0, None, None)

        stream.activity(message="Aligning locked planetary destination", level="info")
        emit_update("ALIGNING", target_name, initial_probe["sample"], status=initial_probe["status"], heat_percent=heat_percent, reason="ESCAPE_VECTOR_NOT_PRESENT_AT_CURRENT_POSITION")
        action.call(id="elite-dangerous/align-station-target", inputs={"mode": "ALIGN", "targetMotion": "STATIC", "trackingSamples": 120, "stopBeforeAlign": True, "controlProfile": "NORMAL_SPACE"})
        visible_attempt = action.try_call(id="elite-dangerous/align-visible-target", inputs={"targetName": target_name, "stopBeforeAlign": False, "centerHintConfirmed": True, "positionSource": "DESTINATION", "heatPolicy": "STRICT"})
        visible_reason = "VISIBLE_TARGET_ALIGNMENT_COMPLETED" if visible_attempt["ok"] else "VISIBLE_TARGET_ALIGNMENT_UNAVAILABLE: " + visible_attempt["error"]
        emit_update("ALIGNING", target_name, initial_probe["sample"], status=initial_probe["status"], heat_percent=heat_percent, reason=visible_reason)

        set_probe_failure_compensation()
        action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
        sample = initial_probe["sample"]
        entered = False
        for _ in range(ENTER_SUPERCRUISE_LIMIT):
            sample += 1
            status = observe_status()
            flight = observe_flight_status()
            emit_update("ENTERING_SUPERCRUISE", target_name, sample, commanded_throttle=100, flight_status=flight["state"], status=status, reason=flight["reason"])
            if status["supercruise"]:
                entered = True
                set_safe_failure_compensation()
                break
            if flight["state"] == "FSD_ESCAPE_VECTOR_REQUIRED":
                probe = cancel_owned_charge(target_name, sample, flight["state"], status, ESCAPE_VECTOR_CONFIRMATIONS_REQUIRED, "ENTERING_SUPERCRUISE")
                result = {"detected": True, "sample": sample, "confirmations": ESCAPE_VECTOR_CONFIRMATIONS_REQUIRED, "status": probe, "flightStatus": flight["state"]}
                return completed_output(target_name, "ALREADY_IN_GRAVITY_WELL", result, 0, None, None)
            task.sleep(milliseconds=500)
        if not entered:
            fail("manual Supercruise entry was not confirmed")

    stream.activity(message="Approaching locked body until gravity-influence candidate telemetry is stable", level="info")
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    previous_distance = None
    candidate_confirmations = 0
    increasing_confirmations = 0
    unknown_count = 0
    final_distance = None
    final_speed = None
    approach_samples = 0
    automatic_drop_status = None
    for _ in range(APPROACH_SAMPLE_LIMIT):
        approach_samples += 1
        sample += 1
        current_status = observe_status()
        if not current_status["supercruise"]:
            action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            automatic_drop_status = current_status
            emit_update("DROPPING", target_name, sample, status=current_status, target_distance=final_distance, speed=final_speed, trend="CANDIDATE" if candidate_confirmations > 0 else "UNKNOWN", candidate_confirmations=candidate_confirmations, reason="GAME_AUTOMATIC_GRAVITY_WELL_DROP_CONFIRMED")
            break
        distance_observation = observe_target_distance()
        speed_observation = observe_supercruise_speed()
        if not distance_observation["ok"] or not speed_observation["ok"]:
            unknown_count += 1
            emit_update("APPROACHING", target_name, sample, commanded_throttle=100, status=current_status, target_distance=distance_observation["value"], speed=speed_observation["value"], reason=distance_observation["reason"] + "; " + speed_observation["reason"])
            if unknown_count >= APPROACH_UNKNOWN_LIMIT:
                fail("Supercruise gravity-approach telemetry remained unavailable")
            task.sleep(milliseconds=APPROACH_POLL_MS)
            continue
        unknown_count = 0
        final_distance = distance_observation["value"]
        final_speed = speed_observation["value"]
        trend = "BASELINE"
        if previous_distance != None:
            epsilon = previous_distance * 0.002
            if epsilon < 1000.0:
                epsilon = 1000.0
            delta = final_distance - previous_distance
            if delta < -epsilon:
                trend = "DECREASING"
            elif delta > epsilon:
                trend = "INCREASING"
            else:
                trend = "STABLE"
        if trend == "INCREASING":
            increasing_confirmations += 1
        else:
            increasing_confirmations = 0
        candidate = final_distance <= GRAVITY_CANDIDATE_DISTANCE_METERS and final_speed <= GRAVITY_CANDIDATE_SPEED_METERS_PER_SECOND and trend != "INCREASING"
        candidate_confirmations = candidate_confirmations + 1 if candidate else 0
        display_trend = "CANDIDATE" if candidate_confirmations > 0 else trend
        emit_update("APPROACHING", target_name, sample, commanded_throttle=100, status=current_status, target_distance=final_distance, speed=final_speed, trend=display_trend, candidate_confirmations=candidate_confirmations, reason="GRAVITY_CANDIDATE_TELEMETRY")
        previous_distance = final_distance
        if increasing_confirmations >= MAX_INCREASING_DISTANCE_SAMPLES:
            action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            emit_update("APPROACHING", target_name, sample, commanded_throttle=0, status=current_status, target_distance=final_distance, speed=final_speed, trend="INCREASING", candidate_confirmations=0, reason="TARGET_DISTANCE_INCREASING_LIMIT_REACHED")
            fail("locked target distance increased for three consecutive approach samples")
        if candidate_confirmations >= GRAVITY_CANDIDATE_CONFIRMATIONS_REQUIRED:
            break
        task.sleep(milliseconds=APPROACH_POLL_MS)
    if automatic_drop_status == None and candidate_confirmations < GRAVITY_CANDIDATE_CONFIRMATIONS_REQUIRED:
        fail("gravity-influence candidate telemetry was not reached before the approach limit")

    if automatic_drop_status == None:
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
        task.sleep(milliseconds=500)
        action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
        emit_update("DROPPING", target_name, sample, status=observe_status(), target_distance=final_distance, speed=final_speed, trend="CANDIDATE", candidate_confirmations=candidate_confirmations, reason="LOW_SPEED_LOW_DISTANCE_CANDIDATE_DROP_SENT")
        normal_status = None
        for _ in range(DROP_CONFIRMATION_LIMIT):
            normal_status = observe_status()
            if not normal_status["supercruise"] and not normal_status["fsdCharging"]:
                break
            task.sleep(milliseconds=POLL_MS)
        if normal_status == None or normal_status["supercruise"] or normal_status["fsdCharging"]:
            fail("normal-space drop was not confirmed after the gravity candidate")

    stream.activity(message="Verifying gravity well with a second bounded Escape Vector probe", level="info")
    final_probe = probe_escape_vector(target_name, sample, "VERIFYING_GRAVITY_WELL")
    if not final_probe["detected"]:
        fail("candidate approach did not produce Escape Vector proof after returning to normal space")
    return completed_output(target_name, entry_mode, final_probe, approach_samples, final_distance, final_speed)
