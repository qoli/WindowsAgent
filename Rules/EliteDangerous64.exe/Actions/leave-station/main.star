POLL_MS = 250
AUTO_LAUNCH_START_LIMIT = 600
AUTO_LAUNCH_HANDOVER_LIMIT = 720
DEPARTURE_LIMIT = 600
UNKNOWN_MASS_LOCK_LIMIT = 20
AUTO_LAUNCH_ABSENCE_STABLE = 5
AUTO_LAUNCH_MOVEMENT_SPEED_MIN = 15
AUTO_LAUNCH_HANDOVER_SPEED_MAX = 10
AUTO_LAUNCH_LOW_SPEED_CONFIRMATIONS = 2
AUTO_LAUNCH_LOW_SPEED_WINDOW = 8
AUTO_LAUNCH_TEMPORAL_LOW_SPEED_CONFIRMATIONS = 4
AUTO_LAUNCH_TEMPORAL_LOW_SPEED_MIN_CONFIDENCE = 0.40
AUTO_LAUNCH_TEMPORAL_LOW_SPEED_MAX_MARGIN = 0.02
MASS_LOCK_OFF_STABLE = 2
STOP_VERIFICATION_LIMIT = 60
ZERO_SPEED_CONFIRMATIONS = 3
ZERO_SPEED_MIN_CONSTRAINED_CONFIDENCE = 0.45
ZERO_SPEED_MAX_RAW_CONSTRAINT_MARGIN = 0.02
MAX_WGC_ERRORS = 5
RETRYABLE_WGC_ERROR_CODES = [
    "capture_device_failed",
    "capture_frame_failed",
    "capture_readback_failed",
    "capture_region_shader_failed",
    "capture_session_failed",
    "capture_timeout",
    "capture_tone_map_failed",
]

def unknown_observation(scope):
    return {
        "observationScope": scope,
        "flightStatus": "UNKNOWN",
        "flightPromptText": None,
        "massLock": "UNKNOWN",
        "observedSpeedState": "UNKNOWN",
        "observedSpeedDisplayValue": None,
        "observedSpeedReason": None,
        "observedSpeedRawText": None,
        "observedSpeedRawConfidence": None,
        "observedSpeedConstrainedText": None,
        "observedSpeedConstrainedConfidence": None,
        "observedSpeedRawConstraintMargin": None,
    }

def parse_speed(speed):
    speed_state = speed["speed"]["state"]
    speed_value = speed["speed"]["displayValue"]
    speed_evidence = speed["speed"]["evidence"]
    if speed_state == "KNOWN" and speed_value == None:
        fail("ship-speed returned KNOWN without displayValue")
    if speed_state == "UNKNOWN" and speed_value != None:
        fail("ship-speed returned UNKNOWN with displayValue")
    return {
        "observedSpeedState": speed_state,
        "observedSpeedDisplayValue": speed_value,
        "observedSpeedReason": speed_evidence["reason"],
        "observedSpeedRawText": speed_evidence["rawText"],
        "observedSpeedRawConfidence": speed_evidence["rawConfidence"],
        "observedSpeedConstrainedText": speed_evidence["constrainedText"],
        "observedSpeedConstrainedConfidence": speed_evidence["constrainedConfidence"],
        "observedSpeedRawConstraintMargin": speed_evidence["rawConstraintMargin"],
    }

def failed_observation(attempt):
    return {"ok": False, "output": None, "error": attempt["error"], "errorCode": attempt["errorCode"]}

def observe():
    raw_attempt = action.try_call(id="elite-dangerous/flight-prompt-text", inputs={})
    if not raw_attempt["ok"]:
        return failed_observation(raw_attempt)
    raw = raw_attempt["output"]
    flight_attempt = action.try_call(id="elite-dangerous/flight-status", inputs=raw)
    if not flight_attempt["ok"]:
        return failed_observation(flight_attempt)
    ship_attempt = action.try_call(id="elite-dangerous/ship-status", inputs={})
    if not ship_attempt["ok"]:
        return failed_observation(ship_attempt)
    speed_attempt = action.try_call(id="elite-dangerous/ship-speed", inputs={})
    if not speed_attempt["ok"]:
        return failed_observation(speed_attempt)
    flight = flight_attempt["output"]
    ship = ship_attempt["output"]
    speed = parse_speed(speed_attempt["output"])
    return {"ok": True, "error": None, "errorCode": None, "output": {
        "observationScope": "FULL",
        "flightStatus": flight["flightStatus"]["state"],
        "flightPromptText": raw["text"],
        "massLock": ship["shipStatus"]["massLock"]["state"],
        "observedSpeedState": speed["observedSpeedState"],
        "observedSpeedDisplayValue": speed["observedSpeedDisplayValue"],
        "observedSpeedReason": speed["observedSpeedReason"],
        "observedSpeedRawText": speed["observedSpeedRawText"],
        "observedSpeedRawConfidence": speed["observedSpeedRawConfidence"],
        "observedSpeedConstrainedText": speed["observedSpeedConstrainedText"],
        "observedSpeedConstrainedConfidence": speed["observedSpeedConstrainedConfidence"],
        "observedSpeedRawConstraintMargin": speed["observedSpeedRawConstraintMargin"],
    }}

def observe_stop_speed():
    speed_attempt = action.try_call(id="elite-dangerous/ship-speed", inputs={})
    if not speed_attempt["ok"]:
        return failed_observation(speed_attempt)
    speed = parse_speed(speed_attempt["output"])
    return {"ok": True, "error": None, "errorCode": None, "output": {
        "observationScope": "SPEED_ONLY",
        "flightStatus": "UNKNOWN",
        "flightPromptText": None,
        "massLock": "UNKNOWN",
        "observedSpeedState": speed["observedSpeedState"],
        "observedSpeedDisplayValue": speed["observedSpeedDisplayValue"],
        "observedSpeedReason": speed["observedSpeedReason"],
        "observedSpeedRawText": speed["observedSpeedRawText"],
        "observedSpeedRawConfidence": speed["observedSpeedRawConfidence"],
        "observedSpeedConstrainedText": speed["observedSpeedConstrainedText"],
        "observedSpeedConstrainedConfidence": speed["observedSpeedConstrainedConfidence"],
        "observedSpeedRawConstraintMargin": speed["observedSpeedRawConstraintMargin"],
    }}

def is_retryable_wgc_error(attempt):
    return attempt["errorCode"] in RETRYABLE_WGC_ERROR_CODES

def is_handover_low_speed_text(text):
    return text in ["0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10"]

def gate_state(auto_launch_seen=False, samples_since_auto_launch_seen=None, movement_seen=False, maximum_observed_speed=None, low_speed_confirmations=0, temporal_low_speed_text=None, temporal_low_speed_confirmations=0, handover_evidence="NONE", handover_candidate=False, decision="WAITING_FOR_AUTO_LAUNCH", samples_since_throttle_zero=None, zero_speed_confirmations=0, stop_decision="NOT_STARTED"):
    return {
        "autoLaunchSeen": auto_launch_seen,
        "samplesSinceAutoLaunchSeen": samples_since_auto_launch_seen,
        "movementSeen": movement_seen,
        "maximumObservedSpeed": maximum_observed_speed,
        "lowSpeedConfirmations": low_speed_confirmations,
        "temporalLowSpeedText": temporal_low_speed_text,
        "temporalLowSpeedConfirmations": temporal_low_speed_confirmations,
        "handoverEvidence": handover_evidence,
        "handoverCandidate": handover_candidate,
        "gateDecision": decision,
        "samplesSinceThrottleZero": samples_since_throttle_zero,
        "zeroSpeedConfirmations": zero_speed_confirmations,
        "stopGateDecision": stop_decision,
    }

def emit_update(phase, sample, observation, gate, commanded_throttle=None, instruction=None, throttle_command=None, observation_error_count=0, observation_error=None):
    stream.emit(
        type="action.leave-station.update",
        payload={
            "phase": phase,
            "sample": sample,
            "observationScope": observation["observationScope"],
            "flightStatus": observation["flightStatus"],
            "flightPromptText": observation["flightPromptText"],
            "massLock": observation["massLock"],
            "observedSpeedState": observation["observedSpeedState"],
            "observedSpeedDisplayValue": observation["observedSpeedDisplayValue"],
            "observedSpeedReason": observation["observedSpeedReason"],
            "observedSpeedRawText": observation["observedSpeedRawText"],
            "observedSpeedRawConfidence": observation["observedSpeedRawConfidence"],
            "observedSpeedConstrainedText": observation["observedSpeedConstrainedText"],
            "observedSpeedConstrainedConfidence": observation["observedSpeedConstrainedConfidence"],
            "observedSpeedRawConstraintMargin": observation["observedSpeedRawConstraintMargin"],
            "autoLaunchSeen": gate["autoLaunchSeen"],
            "samplesSinceAutoLaunchSeen": gate["samplesSinceAutoLaunchSeen"],
            "movementSeen": gate["movementSeen"],
            "maximumObservedSpeed": gate["maximumObservedSpeed"],
            "lowSpeedConfirmations": gate["lowSpeedConfirmations"],
            "temporalLowSpeedText": gate["temporalLowSpeedText"],
            "temporalLowSpeedConfirmations": gate["temporalLowSpeedConfirmations"],
            "handoverEvidence": gate["handoverEvidence"],
            "handoverCandidate": gate["handoverCandidate"],
            "gateDecision": gate["gateDecision"],
            "samplesSinceThrottleZero": gate["samplesSinceThrottleZero"],
            "zeroSpeedConfirmations": gate["zeroSpeedConfirmations"],
            "stopGateDecision": gate["stopGateDecision"],
            "commandedThrottle": commanded_throttle,
            "instruction": instruction,
            "throttleCommand": throttle_command,
            "observationErrorCount": observation_error_count,
            "observationError": observation_error,
        },
    )

def main(ctx):
    if ctx.inputs["stationConfirmed"] != True:
        fail("stationConfirmed must be true")

    sample = 0
    unknown = unknown_observation("NONE")
    gate = gate_state()
    wgc_error_count = 0
    emit_update(
        "AWAITING_AUTO_LAUNCH",
        sample,
        unknown,
        gate,
        instruction="Use elite-dangerous/ui-control with screenshot feedback to select AUTO LAUNCH.",
    )

    auto_launch_seen = False
    unknown_mass_lock_count = 0
    for _ in range(AUTO_LAUNCH_START_LIMIT):
        attempt = observe()
        sample += 1
        if not attempt["ok"]:
            if not is_retryable_wgc_error(attempt):
                fail(attempt["error"])
            wgc_error_count += 1
            if wgc_error_count > MAX_WGC_ERRORS:
                fail("WGC observation error limit exceeded after five skipped errors: " + attempt["error"])
            emit_update("OBSERVATION_ERROR", sample, unknown_observation("FULL"), gate, observation_error_count=wgc_error_count, observation_error=attempt["error"])
            task.sleep(milliseconds=POLL_MS)
            continue
        observation = attempt["output"]
        if observation["massLock"] == "OFF":
            fail("Mass Lock became OFF before Auto Launch was observed")
        if observation["massLock"] == "UNKNOWN":
            unknown_mass_lock_count += 1
            if unknown_mass_lock_count >= UNKNOWN_MASS_LOCK_LIMIT:
                fail("Mass Lock remained UNKNOWN while awaiting Auto Launch")
        else:
            unknown_mass_lock_count = 0
        if observation["flightStatus"] == "AUTO_LAUNCH" or observation["flightStatus"] == "WAITING_IN_QUEUE":
            auto_launch_seen = True
            gate = gate_state(auto_launch_seen=True, samples_since_auto_launch_seen=0, decision="AUTO_LAUNCH_VISIBLE")
            emit_update("AWAITING_AUTO_LAUNCH", sample, observation, gate)
            break
        if observation["flightStatus"] != "UNKNOWN":
            fail("unexpected known flight status while awaiting Auto Launch: " + observation["flightStatus"])
        emit_update("AWAITING_AUTO_LAUNCH", sample, observation, gate)
        task.sleep(milliseconds=POLL_MS)
    if not auto_launch_seen:
        fail("Auto Launch was not observed before the sample limit")

    samples_since_auto_launch_seen = 0
    movement_seen = observation["observedSpeedState"] == "KNOWN" and observation["observedSpeedDisplayValue"] >= AUTO_LAUNCH_MOVEMENT_SPEED_MIN
    maximum_observed_speed = observation["observedSpeedDisplayValue"] if observation["observedSpeedState"] == "KNOWN" else None
    low_speed_samples = []
    temporal_low_speed_text = None
    temporal_low_speed_confirmations = 0
    handover_evidence = "NONE"
    handover_confirmed = False
    unknown_mass_lock_count = 0
    for _ in range(AUTO_LAUNCH_HANDOVER_LIMIT):
        attempt = observe()
        sample += 1
        if not attempt["ok"]:
            if not is_retryable_wgc_error(attempt):
                fail(attempt["error"])
            wgc_error_count += 1
            if wgc_error_count > MAX_WGC_ERRORS:
                fail("WGC observation error limit exceeded after five skipped errors: " + attempt["error"])
            emit_update("OBSERVATION_ERROR", sample, unknown_observation("FULL"), gate, observation_error_count=wgc_error_count, observation_error=attempt["error"])
            task.sleep(milliseconds=POLL_MS)
            continue
        observation = attempt["output"]
        mass_lock = observation["massLock"]
        if mass_lock == "UNKNOWN":
            unknown_mass_lock_count += 1
            if unknown_mass_lock_count >= UNKNOWN_MASS_LOCK_LIMIT:
                fail("Mass Lock remained UNKNOWN during Auto Launch")
        else:
            unknown_mass_lock_count = 0
        if mass_lock == "OFF":
            fail("Mass Lock became OFF before throttle 100 was commanded")
        flight_status = observation["flightStatus"]
        if flight_status == "AUTO_LAUNCH" or flight_status == "WAITING_IN_QUEUE":
            samples_since_auto_launch_seen = 0
            low_speed_samples = []
            temporal_low_speed_text = None
            temporal_low_speed_confirmations = 0
        elif flight_status == "UNKNOWN":
            samples_since_auto_launch_seen += 1
        else:
            fail("unexpected known flight status during Auto Launch: " + flight_status)

        if observation["observedSpeedState"] == "KNOWN":
            speed_value = observation["observedSpeedDisplayValue"]
            if maximum_observed_speed == None or speed_value > maximum_observed_speed:
                maximum_observed_speed = speed_value
            if speed_value >= AUTO_LAUNCH_MOVEMENT_SPEED_MIN:
                movement_seen = True

        handover_candidate = movement_seen and samples_since_auto_launch_seen >= AUTO_LAUNCH_ABSENCE_STABLE and mass_lock == "ON"
        if handover_candidate and observation["observedSpeedState"] == "KNOWN":
            if observation["observedSpeedDisplayValue"] <= AUTO_LAUNCH_HANDOVER_SPEED_MAX:
                low_speed_samples.append(sample)
            else:
                low_speed_samples = []

        temporal_low_speed_qualified = (
            handover_candidate and
            observation["observedSpeedState"] == "UNKNOWN" and
            observation["observedSpeedReason"] == "CONSTRAINED_CONFIDENCE_LOW" and
            observation["observedSpeedRawText"] == observation["observedSpeedConstrainedText"] and
            is_handover_low_speed_text(observation["observedSpeedConstrainedText"]) and
            observation["observedSpeedConstrainedConfidence"] >= AUTO_LAUNCH_TEMPORAL_LOW_SPEED_MIN_CONFIDENCE and
            observation["observedSpeedRawConstraintMargin"] <= AUTO_LAUNCH_TEMPORAL_LOW_SPEED_MAX_MARGIN
        )
        if temporal_low_speed_qualified:
            candidate_text = observation["observedSpeedConstrainedText"]
            if candidate_text == temporal_low_speed_text:
                temporal_low_speed_confirmations += 1
            else:
                temporal_low_speed_text = candidate_text
                temporal_low_speed_confirmations = 1
        else:
            temporal_low_speed_text = None
            temporal_low_speed_confirmations = 0

        recent_low_speed_samples = []
        for low_speed_sample in low_speed_samples:
            if sample - low_speed_sample < AUTO_LAUNCH_LOW_SPEED_WINDOW:
                recent_low_speed_samples.append(low_speed_sample)
        low_speed_samples = recent_low_speed_samples

        decision = "WAITING_FOR_MOVEMENT"
        if flight_status == "AUTO_LAUNCH" or flight_status == "WAITING_IN_QUEUE":
            decision = "AUTO_LAUNCH_VISIBLE"
        elif not movement_seen:
            decision = "WAITING_FOR_MOVEMENT"
        elif samples_since_auto_launch_seen < AUTO_LAUNCH_ABSENCE_STABLE:
            decision = "WAITING_FOR_AUTO_LAUNCH_ABSENCE"
        elif mass_lock != "ON":
            decision = "WAITING_FOR_MASS_LOCK"
        elif len(low_speed_samples) >= AUTO_LAUNCH_LOW_SPEED_CONFIRMATIONS:
            decision = "HANDOVER_CONFIRMED"
            handover_evidence = "STRICT_SINGLE_FRAME"
            handover_confirmed = True
        elif temporal_low_speed_confirmations >= AUTO_LAUNCH_TEMPORAL_LOW_SPEED_CONFIRMATIONS:
            decision = "HANDOVER_CONFIRMED"
            handover_evidence = "TEMPORAL_LOW_CONFIDENCE"
            handover_confirmed = True
        else:
            decision = "WAITING_FOR_LOW_SPEED_CONFIRMATION"

        gate = gate_state(
            auto_launch_seen=True,
            samples_since_auto_launch_seen=samples_since_auto_launch_seen,
            movement_seen=movement_seen,
            maximum_observed_speed=maximum_observed_speed,
            low_speed_confirmations=len(low_speed_samples),
            temporal_low_speed_text=temporal_low_speed_text,
            temporal_low_speed_confirmations=temporal_low_speed_confirmations,
            handover_evidence=handover_evidence,
            handover_candidate=handover_candidate,
            decision=decision,
        )
        phase = "HANDOVER_CANDIDATE" if handover_candidate else "AUTO_LAUNCH_ACTIVE"
        emit_update(phase, sample, observation, gate)
        if handover_confirmed:
            break
        task.sleep(milliseconds=POLL_MS)
    if not handover_confirmed:
        fail("Auto Launch visual handover was not confirmed before the sample limit")

    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    throttle_100 = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    emit_update("DEPARTING", sample, observation, gate, commanded_throttle=100, throttle_command=throttle_100)

    mass_lock_off_count = 0
    unknown_mass_lock_count = 0
    for _ in range(DEPARTURE_LIMIT):
        attempt = observe()
        sample += 1
        if not attempt["ok"]:
            if not is_retryable_wgc_error(attempt):
                fail(attempt["error"])
            wgc_error_count += 1
            if wgc_error_count > MAX_WGC_ERRORS:
                fail("WGC observation error limit exceeded after five skipped errors: " + attempt["error"])
            emit_update("OBSERVATION_ERROR", sample, unknown_observation("FULL"), gate, commanded_throttle=100, observation_error_count=wgc_error_count, observation_error=attempt["error"])
            task.sleep(milliseconds=POLL_MS)
            continue
        observation = attempt["output"]
        mass_lock = observation["massLock"]
        if mass_lock == "UNKNOWN":
            unknown_mass_lock_count += 1
            mass_lock_off_count = 0
            if unknown_mass_lock_count >= UNKNOWN_MASS_LOCK_LIMIT:
                fail("Mass Lock remained UNKNOWN during departure")
        elif mass_lock == "OFF":
            unknown_mass_lock_count = 0
            mass_lock_off_count += 1
        else:
            unknown_mass_lock_count = 0
            mass_lock_off_count = 0
        departure_decision = "MASS_LOCK_RELEASE_CONFIRMED" if mass_lock_off_count >= MASS_LOCK_OFF_STABLE else "WAITING_FOR_MASS_LOCK_RELEASE"
        gate = gate_state(
            auto_launch_seen=True,
            samples_since_auto_launch_seen=samples_since_auto_launch_seen,
            movement_seen=movement_seen,
            maximum_observed_speed=maximum_observed_speed,
            low_speed_confirmations=len(low_speed_samples),
            temporal_low_speed_text=temporal_low_speed_text,
            temporal_low_speed_confirmations=temporal_low_speed_confirmations,
            handover_evidence=handover_evidence,
            handover_candidate=True,
            decision=departure_decision,
        )
        emit_update("DEPARTING", sample, observation, gate, commanded_throttle=100)
        if mass_lock_off_count >= MASS_LOCK_OFF_STABLE:
            throttle_0 = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            action.clear_on_failure()
            gate = gate_state(
                auto_launch_seen=True,
                samples_since_auto_launch_seen=samples_since_auto_launch_seen,
                movement_seen=movement_seen,
                maximum_observed_speed=maximum_observed_speed,
                low_speed_confirmations=len(low_speed_samples),
                temporal_low_speed_text=temporal_low_speed_text,
                temporal_low_speed_confirmations=temporal_low_speed_confirmations,
                handover_evidence=handover_evidence,
                handover_candidate=True,
                decision="MASS_LOCK_RELEASE_CONFIRMED",
                samples_since_throttle_zero=0,
                zero_speed_confirmations=0,
                stop_decision="WAITING_FOR_ZERO_SPEED",
            )
            emit_update("VERIFYING_STOP", sample, observation, gate, commanded_throttle=0, throttle_command=throttle_0)

            zero_speed_confirmations = 0
            for samples_since_throttle_zero in range(1, STOP_VERIFICATION_LIMIT + 1):
                attempt = observe_stop_speed()
                sample += 1
                if not attempt["ok"]:
                    if not is_retryable_wgc_error(attempt):
                        fail(attempt["error"])
                    wgc_error_count += 1
                    if wgc_error_count > MAX_WGC_ERRORS:
                        fail("WGC observation error limit exceeded after five skipped errors: " + attempt["error"])
                    emit_update("OBSERVATION_ERROR", sample, unknown_observation("SPEED_ONLY"), gate, commanded_throttle=0, observation_error_count=wgc_error_count, observation_error=attempt["error"])
                    task.sleep(milliseconds=POLL_MS)
                    continue
                observation = attempt["output"]

                weak_zero = (
                    (observation["observedSpeedState"] != "KNOWN" or observation["observedSpeedDisplayValue"] == 0) and
                    observation["observedSpeedRawText"] == "0" and
                    observation["observedSpeedConstrainedText"] == "0" and
                    observation["observedSpeedConstrainedConfidence"] >= ZERO_SPEED_MIN_CONSTRAINED_CONFIDENCE and
                    observation["observedSpeedRawConstraintMargin"] <= ZERO_SPEED_MAX_RAW_CONSTRAINT_MARGIN
                )
                if weak_zero:
                    zero_speed_confirmations += 1
                else:
                    zero_speed_confirmations = 0

                stop_decision = "ZERO_SPEED_CONFIRMED" if zero_speed_confirmations >= ZERO_SPEED_CONFIRMATIONS else "WAITING_FOR_ZERO_SPEED"
                gate = gate_state(
                    auto_launch_seen=True,
                    samples_since_auto_launch_seen=samples_since_auto_launch_seen,
                    movement_seen=movement_seen,
                    maximum_observed_speed=maximum_observed_speed,
                    low_speed_confirmations=len(low_speed_samples),
                    temporal_low_speed_text=temporal_low_speed_text,
                    temporal_low_speed_confirmations=temporal_low_speed_confirmations,
                    handover_evidence=handover_evidence,
                    handover_candidate=True,
                    decision="MASS_LOCK_RELEASE_CONFIRMED",
                    samples_since_throttle_zero=samples_since_throttle_zero,
                    zero_speed_confirmations=zero_speed_confirmations,
                    stop_decision=stop_decision,
                )
                phase = "COMPLETED" if stop_decision == "ZERO_SPEED_CONFIRMED" else "VERIFYING_STOP"
                emit_update(phase, sample, observation, gate, commanded_throttle=0)
                if stop_decision == "ZERO_SPEED_CONFIRMED":
                    return {
                        "schemaVersion": 3,
                        "task": "LEAVE_STATION",
                        "completed": True,
                        "finalPhase": "COMPLETED",
                        "finalMassLock": "OFF",
                        "finalCommandedThrottle": 0,
                        "finalStopState": "CONFIRMED",
                        "zeroSpeedConfirmations": zero_speed_confirmations,
                        "lastObservedSpeedState": observation["observedSpeedState"],
                        "lastObservedSpeedDisplayValue": observation["observedSpeedDisplayValue"],
                        "lastObservedSpeedRawText": observation["observedSpeedRawText"],
                        "lastObservedSpeedConstrainedText": observation["observedSpeedConstrainedText"],
                        "lastObservedSpeedConstrainedConfidence": observation["observedSpeedConstrainedConfidence"],
                        "lastObservedSpeedRawConstraintMargin": observation["observedSpeedRawConstraintMargin"],
                        "sampleCount": sample,
                    }
                task.sleep(milliseconds=POLL_MS)
            fail("Zero speed was not visually confirmed after throttle 0")
        task.sleep(milliseconds=POLL_MS)
    fail("Mass Lock did not become OFF before the departure sample limit")
