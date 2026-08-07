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
MASS_LOCK_OFF_STABLE = 2

def observe():
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    flight = action.call(id="elite-dangerous/flight-status", inputs=raw)
    ship = action.call(id="elite-dangerous/ship-status", inputs={})
    speed = action.call(id="elite-dangerous/ship-speed", inputs={})
    speed_state = speed["speed"]["state"]
    speed_value = speed["speed"]["displayValue"]
    speed_evidence = speed["speed"]["evidence"]
    if speed_state == "KNOWN" and speed_value == None:
        fail("ship-speed returned KNOWN without displayValue")
    if speed_state == "UNKNOWN" and speed_value != None:
        fail("ship-speed returned UNKNOWN with displayValue")
    return {
        "flightStatus": flight["flightStatus"]["state"],
        "flightPromptText": raw["text"],
        "massLock": ship["shipStatus"]["massLock"]["state"],
        "observedSpeedState": speed_state,
        "observedSpeedDisplayValue": speed_value,
        "observedSpeedReason": speed_evidence["reason"],
        "observedSpeedRawText": speed_evidence["rawText"],
        "observedSpeedDetectionConfidence": speed_evidence["detectionConfidence"],
        "observedSpeedRecognitionConfidence": speed_evidence["recognitionConfidence"],
    }

def gate_state(auto_launch_seen=False, samples_since_auto_launch_seen=None, movement_seen=False, maximum_observed_speed=None, low_speed_confirmations=0, handover_candidate=False, decision="WAITING_FOR_AUTO_LAUNCH"):
    return {
        "autoLaunchSeen": auto_launch_seen,
        "samplesSinceAutoLaunchSeen": samples_since_auto_launch_seen,
        "movementSeen": movement_seen,
        "maximumObservedSpeed": maximum_observed_speed,
        "lowSpeedConfirmations": low_speed_confirmations,
        "handoverCandidate": handover_candidate,
        "gateDecision": decision,
    }

def emit_update(phase, sample, observation, gate, commanded_throttle=None, instruction=None, throttle_command=None):
    stream.emit(
        type="action.leave-station.update",
        payload={
            "phase": phase,
            "sample": sample,
            "flightStatus": observation["flightStatus"],
            "flightPromptText": observation["flightPromptText"],
            "massLock": observation["massLock"],
            "observedSpeedState": observation["observedSpeedState"],
            "observedSpeedDisplayValue": observation["observedSpeedDisplayValue"],
            "observedSpeedReason": observation["observedSpeedReason"],
            "observedSpeedRawText": observation["observedSpeedRawText"],
            "observedSpeedDetectionConfidence": observation["observedSpeedDetectionConfidence"],
            "observedSpeedRecognitionConfidence": observation["observedSpeedRecognitionConfidence"],
            "autoLaunchSeen": gate["autoLaunchSeen"],
            "samplesSinceAutoLaunchSeen": gate["samplesSinceAutoLaunchSeen"],
            "movementSeen": gate["movementSeen"],
            "maximumObservedSpeed": gate["maximumObservedSpeed"],
            "lowSpeedConfirmations": gate["lowSpeedConfirmations"],
            "handoverCandidate": gate["handoverCandidate"],
            "gateDecision": gate["gateDecision"],
            "commandedThrottle": commanded_throttle,
            "instruction": instruction,
            "throttleCommand": throttle_command,
        },
    )

def main(ctx):
    if ctx.inputs["stationConfirmed"] != True:
        fail("stationConfirmed must be true")

    sample = 0
    unknown = {
        "flightStatus": "UNKNOWN",
        "flightPromptText": None,
        "massLock": "UNKNOWN",
        "observedSpeedState": "UNKNOWN",
        "observedSpeedDisplayValue": None,
        "observedSpeedReason": None,
        "observedSpeedRawText": None,
        "observedSpeedDetectionConfidence": None,
        "observedSpeedRecognitionConfidence": None,
    }
    gate = gate_state()
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
        observation = observe()
        sample += 1
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
    handover_confirmed = False
    unknown_mass_lock_count = 0
    for _ in range(AUTO_LAUNCH_HANDOVER_LIMIT):
        observation = observe()
        sample += 1
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
        elif len(low_speed_samples) < AUTO_LAUNCH_LOW_SPEED_CONFIRMATIONS:
            decision = "WAITING_FOR_LOW_SPEED_CONFIRMATION"
        else:
            decision = "HANDOVER_CONFIRMED"
            handover_confirmed = True

        gate = gate_state(
            auto_launch_seen=True,
            samples_since_auto_launch_seen=samples_since_auto_launch_seen,
            movement_seen=movement_seen,
            maximum_observed_speed=maximum_observed_speed,
            low_speed_confirmations=len(low_speed_samples),
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

    throttle_100 = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    emit_update("DEPARTING", sample, observation, gate, commanded_throttle=100, throttle_command=throttle_100)

    mass_lock_off_count = 0
    unknown_mass_lock_count = 0
    for _ in range(DEPARTURE_LIMIT):
        observation = observe()
        sample += 1
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
            handover_candidate=True,
            decision=departure_decision,
        )
        emit_update("DEPARTING", sample, observation, gate, commanded_throttle=100)
        if mass_lock_off_count >= MASS_LOCK_OFF_STABLE:
            throttle_0 = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            emit_update("COMPLETED", sample, observation, gate, commanded_throttle=0, throttle_command=throttle_0)
            return {
                "schemaVersion": 2,
                "task": "LEAVE_STATION",
                "completed": True,
                "finalPhase": "COMPLETED",
                "finalMassLock": "OFF",
                "finalCommandedThrottle": 0,
                "lastObservedSpeedState": observation["observedSpeedState"],
                "lastObservedSpeedDisplayValue": observation["observedSpeedDisplayValue"],
                "sampleCount": sample,
            }
        task.sleep(milliseconds=POLL_MS)
    fail("Mass Lock did not become OFF before the departure sample limit")
