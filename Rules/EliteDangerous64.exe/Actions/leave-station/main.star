POLL_MS = 250
AUTO_LAUNCH_START_LIMIT = 600
AUTO_LAUNCH_HANDOVER_LIMIT = 720
DEPARTURE_LIMIT = 600
UNKNOWN_MASS_LOCK_LIMIT = 20
AUTO_LAUNCH_HANDOVER_STABLE = 3
MASS_LOCK_OFF_STABLE = 2

def observe():
    raw = action.call(id="elite-dangerous/flight-prompt-text", inputs={})
    flight = action.call(id="elite-dangerous/flight-status", inputs=raw)
    ship = action.call(id="elite-dangerous/ship-status", inputs={})
    speed = action.call(id="elite-dangerous/ship-speed", inputs={})
    speed_state = speed["speed"]["state"]
    speed_value = speed["speed"]["displayValue"]
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
    }

def emit_update(phase, sample, observation, commanded_throttle=None, instruction=None, throttle_command=None):
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
    }
    emit_update(
        "AWAITING_AUTO_LAUNCH",
        sample,
        unknown,
        instruction="Use elite-dangerous/ui-control with screenshot feedback to select AUTO LAUNCH.",
    )

    auto_launch_seen = False
    unknown_mass_lock_count = 0
    for _ in range(AUTO_LAUNCH_START_LIMIT):
        observation = observe()
        sample += 1
        emit_update("AWAITING_AUTO_LAUNCH", sample, observation)
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
            break
        if observation["flightStatus"] != "UNKNOWN":
            fail("unexpected known flight status while awaiting Auto Launch: " + observation["flightStatus"])
        task.sleep(milliseconds=POLL_MS)
    if not auto_launch_seen:
        fail("Auto Launch was not observed before the sample limit")

    handover_visual_count = 0
    unknown_mass_lock_count = 0
    for _ in range(AUTO_LAUNCH_HANDOVER_LIMIT):
        observation = observe()
        sample += 1
        emit_update("AUTO_LAUNCH_ACTIVE", sample, observation)
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
            handover_visual_count = 0
        elif flight_status == "UNKNOWN":
            if (
                observation["flightPromptText"] == "" and
                observation["observedSpeedState"] == "KNOWN" and
                observation["observedSpeedDisplayValue"] > 0 and
                mass_lock == "ON"
            ):
                handover_visual_count += 1
            else:
                handover_visual_count = 0
        else:
            fail("unexpected known flight status during Auto Launch: " + flight_status)
        if handover_visual_count >= AUTO_LAUNCH_HANDOVER_STABLE:
            break
        task.sleep(milliseconds=POLL_MS)
    if handover_visual_count < AUTO_LAUNCH_HANDOVER_STABLE:
        fail("Auto Launch visual handover was not confirmed before the sample limit")

    throttle_100 = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    emit_update("DEPARTING", sample, observation, commanded_throttle=100, throttle_command=throttle_100)

    mass_lock_off_count = 0
    unknown_mass_lock_count = 0
    for _ in range(DEPARTURE_LIMIT):
        observation = observe()
        sample += 1
        emit_update("DEPARTING", sample, observation, commanded_throttle=100)
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
        if mass_lock_off_count >= MASS_LOCK_OFF_STABLE:
            throttle_0 = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            emit_update("COMPLETED", sample, observation, commanded_throttle=0, throttle_command=throttle_0)
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
