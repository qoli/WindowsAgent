POLL_MS = 250
CHARGING_LIMIT = 80
TRANSIT_ABSENT_CONFIRMATIONS = 2
ARRIVAL_PRESENT_CONFIRMATIONS = 2
ARRIVAL_LIMIT = 240
SUPERCRUISE_HUD_CONFIRMATIONS = 2
SYSTEM_TEXT_CONFIRMATIONS = 2
SYSTEM_TEXT_LIMIT = 20

def emit_update(phase, sample, target_name=None, child_action=None, hyperspace_state=None, flight_status=None, cockpit_hud=None, commanded_throttle=None, last_command=None, reason=None):
    stream.emit(
        type="action.inter-system-transit-to-station.update",
        payload={
            "phase": phase,
            "sample": sample,
            "targetName": target_name,
            "childAction": child_action,
            "hyperspaceState": hyperspace_state,
            "flightStatus": flight_status,
            "cockpitHud": cockpit_hud,
            "commandedThrottle": commanded_throttle,
            "lastCommand": last_command,
            "reason": reason,
        },
    )

def align_target(target_name, sample, phase):
    emit_update(phase, sample, target_name=target_name, child_action="elite-dangerous/align-station-target", commanded_throttle=0, reason="COMPASS_COARSE_ALIGNMENT")
    coarse = action.call(id="elite-dangerous/align-station-target", inputs={"mode": "ALIGN", "stopBeforeAlign": True, "controlProfile": "NORMAL_SPACE"})
    emit_update(phase, sample, target_name=target_name, child_action="elite-dangerous/align-visible-target", commanded_throttle=0, reason="VISIBLE_TARGET_FINE_ALIGNMENT")
    fine = action.call(id="elite-dangerous/align-visible-target", inputs={"targetName": target_name, "stopBeforeAlign": False})
    return {"coarseSamples": coarse["sampleCount"], "fineSamples": fine["sampleCount"]}

def observe_hyperspace():
    observation = action.call(id="elite-dangerous/hyperspace-state", inputs={})["hyperspaceState"]
    return {
        "state": observation["state"],
        "flightStatus": observation["flightStatus"],
        "cockpitHud": observation["cockpitHud"]["state"],
        "promptText": observation["promptText"],
    }

def require_supercruise_hud(sample, target_name):
    confirmations = 0
    for _ in range(8):
        hud = action.call(id="elite-dangerous/supercruise-hud-state", inputs={})["supercruiseHud"]
        if hud["state"] == "ACTIVE":
            confirmations += 1
        else:
            confirmations = 0
        emit_update("CONFIRMING_ARRIVAL", sample, target_name=target_name, child_action="elite-dangerous/supercruise-hud-state", commanded_throttle=0, reason="SUPERCRUISE_HUD_" + hud["state"])
        if confirmations >= SUPERCRUISE_HUD_CONFIRMATIONS:
            return confirmations
        task.sleep(milliseconds=POLL_MS)
    fail("persistent Supercruise HUD was not confirmed after hyperspace cockpit return")

def require_destination_system_text(sample, target_name):
    confirmations = 0
    last = None
    for _ in range(SYSTEM_TEXT_LIMIT):
        last = action.call(id="elite-dangerous/supercruise-target-position", inputs={"targetName": target_name})["target"]
        if last["state"] == "DETECTED":
            confirmations += 1
        else:
            confirmations = 0
        emit_update("CONFIRMING_DESTINATION_SYSTEM", sample, target_name=target_name, child_action="elite-dangerous/supercruise-target-position", commanded_throttle=0, reason=last["reason"])
        if confirmations >= SYSTEM_TEXT_CONFIRMATIONS:
            return {"confirmations": confirmations, "finalTarget": last}
        task.sleep(milliseconds=POLL_MS)
    fail("destination system target text was not visually confirmed after hyperspace arrival")

def main(ctx):
    destination_system = ctx.inputs["destinationSystem"]
    destination_station = ctx.inputs["destinationStation"]
    start_mode = ctx.inputs["startMode"]
    if not ctx.inputs["stationCompatibilityConfirmed"]:
        fail("stationCompatibilityConfirmed must be true before inter-system transit")
    if not ctx.inputs["autoThrottleConfirmed"]:
        fail("autoThrottleConfirmed must be true for Supercruise Assist")
    if start_mode == "NORMAL_SPACE" and not ctx.inputs["normalSpaceConfirmed"]:
        fail("normalSpaceConfirmed must be true for NORMAL_SPACE startMode")
    if start_mode == "DOCKED" and ctx.inputs["normalSpaceConfirmed"]:
        fail("normalSpaceConfirmed must be false for DOCKED startMode")

    sample = 0
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    if start_mode == "DOCKED":
        stream.activity(message="Leaving origin station", level="info")
        emit_update("DEPARTURE", sample, child_action="elite-dangerous/leave-station", reason="AWAITING_CHILD_WORKFLOW")
        action.call(id="elite-dangerous/leave-station", inputs={"stationConfirmed": True})
        emit_update("DEPARTURE_COMPLETED", sample, child_action="elite-dangerous/leave-station", commanded_throttle=0, reason="NORMAL_SPACE_STOP_CONFIRMED")

    stream.activity(message="Locking destination system", level="info")
    emit_update("LOCKING_SYSTEM", sample, target_name=destination_system, child_action="elite-dangerous/select-and-lock-destination", commanded_throttle=0)
    system_lock = action.call(id="elite-dangerous/select-and-lock-destination", inputs={"targetName": destination_system})
    if not system_lock["targetLocked"]:
        fail("destination system lock Action did not confirm targetLocked")
    emit_update("SYSTEM_LOCKED", sample, target_name=destination_system, child_action="elite-dangerous/select-and-lock-destination", commanded_throttle=0, reason=system_lock["result"])

    stream.activity(message="Aligning hyperspace destination", level="info")
    initial_alignment = align_target(destination_system, sample, "ALIGNING_SYSTEM")
    initial = observe_hyperspace()
    if initial["cockpitHud"] != "PRESENT":
        fail("cockpit HUD was not present before hyperspace control")

    jump = action.call(id="elite-dangerous/hyperspace-control", inputs={"command": "JUMP"})
    emit_update("AWAITING_FSD_CHARGING", sample, target_name=destination_system, child_action="elite-dangerous/hyperspace-control", hyperspace_state=initial["state"], flight_status=initial["flightStatus"], cockpit_hud=initial["cockpitHud"], commanded_throttle=0, last_command="HYPERSPACE_JUMP", reason=jump["control"])

    charging_seen = False
    transit_absent = 0
    throttle_100_sent = False
    recovery_alignment = None
    for _ in range(CHARGING_LIMIT):
        sample += 1
        observation = observe_hyperspace()
        state = observation["state"]
        command = None
        phase = "AWAITING_FSD_CHARGING"
        reason = observation["promptText"]
        if state == "FSD_CHARGING":
            charging_seen = True
            phase = "FSD_CHARGING"
            if not throttle_100_sent:
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
                throttle_100_sent = True
                command = "SET_THROTTLE_100"
                reason = throttle["control"]
        elif state == "ALIGNMENT_REQUIRED":
            charging_seen = True
            throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            emit_update("REALIGNING_SYSTEM", sample, target_name=destination_system, hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=0, last_command="SET_THROTTLE_0", reason=throttle["control"])
            recovery_alignment = align_target(destination_system, sample, "REALIGNING_SYSTEM")
            throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
            throttle_100_sent = True
            command = "REALIGN_TARGETS+SET_THROTTLE_100"
            phase = "REALIGNING_SYSTEM"
            reason = throttle["control"]
        elif state == "COCKPIT_ABSENT":
            if not charging_seen:
                fail("cockpit HUD disappeared before FSD charging was visually confirmed")
            transit_absent += 1
            phase = "HYPERSPACE_TRANSIT_CANDIDATE"
        else:
            transit_absent = 0
        emit_update(phase, sample, target_name=destination_system, hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=100 if throttle_100_sent else 0, last_command=command, reason=reason)
        if transit_absent >= TRANSIT_ABSENT_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if not charging_seen or transit_absent < TRANSIT_ABSENT_CONFIRMATIONS:
        fail("FSD charging followed by stable hyperspace cockpit absence was not confirmed")
    stream.activity(message="Hyperspace transit confirmed", level="info")
    emit_update("HYPERSPACE_TRANSIT", sample, target_name=destination_system, hyperspace_state="COCKPIT_ABSENT", cockpit_hud="ABSENT", commanded_throttle=100, reason="TWO_CONSECUTIVE_COCKPIT_ABSENT_SAMPLES")

    arrival_present = 0
    arrival_stop_sent = False
    for _ in range(ARRIVAL_LIMIT):
        sample += 1
        observation = observe_hyperspace()
        command = None
        if observation["cockpitHud"] == "PRESENT":
            arrival_present += 1
            if arrival_present >= ARRIVAL_PRESENT_CONFIRMATIONS and not arrival_stop_sent:
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                arrival_stop_sent = True
                command = "SET_THROTTLE_0"
        else:
            arrival_present = 0
        emit_update("AWAITING_COCKPIT_RETURN", sample, target_name=destination_system, hyperspace_state=observation["state"], flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=0 if arrival_stop_sent else 100, last_command=command, reason=observation["promptText"])
        if arrival_present >= ARRIVAL_PRESENT_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if arrival_present < ARRIVAL_PRESENT_CONFIRMATIONS or not arrival_stop_sent:
        fail("stable cockpit return was not confirmed after hyperspace transit")

    supercruise_confirmations = require_supercruise_hud(sample, destination_system)
    system_confirmation = require_destination_system_text(sample, destination_system)
    stream.activity(message="Destination system arrival confirmed", level="info")
    emit_update("DESTINATION_SYSTEM_CONFIRMED", sample, target_name=destination_system, commanded_throttle=0, reason="COCKPIT_RETURN+SUPERCRUISE_HUD+TARGET_TEXT")

    stream.activity(message="Locking destination station", level="info")
    emit_update("LOCKING_STATION", sample, target_name=destination_station, child_action="elite-dangerous/select-and-lock-destination", commanded_throttle=0)
    station_lock = action.call(id="elite-dangerous/select-and-lock-destination", inputs={"targetName": destination_station})
    if not station_lock["targetLocked"]:
        fail("destination station lock Action did not confirm targetLocked")
    emit_update("STATION_LOCKED", sample, target_name=destination_station, child_action="elite-dangerous/select-and-lock-destination", commanded_throttle=0, reason=station_lock["result"])

    stream.activity(message="Starting Supercruise Assist to station", level="info")
    emit_update("SUPERCRUISE_TO_STATION", sample, target_name=destination_station, child_action="elite-dangerous/supercruise-assist-to-destination", commanded_throttle=0)
    supercruise = action.call(id="elite-dangerous/supercruise-assist-to-destination", inputs={
        "targetName": destination_station,
        "targetLocked": True,
        "normalSpaceConfirmed": False,
        "supercruiseConfirmed": True,
        "assistRequestedConfirmed": False,
        "autoThrottleConfirmed": True,
        "destinationMode": "DROP",
    })
    if not supercruise["completed"]:
        fail("Supercruise Assist child did not complete")

    stream.activity(message="Starting station docking", level="info")
    emit_update("DOCKING", sample, target_name=destination_station, child_action="elite-dangerous/dock-at-station", commanded_throttle=0)
    docking = action.call(id="elite-dangerous/dock-at-station", inputs={})
    if not docking["completed"] or docking["finalPhase"] != "VISUAL_CONFIRMATION_REQUIRED":
        fail("dock-at-station did not reach visual confirmation handoff")
    action.clear_on_failure()
    stream.activity(message="Inter-system transit visual confirmation required", level="warning")
    emit_update("VISUAL_CONFIRMATION_REQUIRED", sample, target_name=destination_station, child_action="elite-dangerous/dock-at-station", commanded_throttle=0, reason="DOCK_AT_STATION_COMPLETED")
    return {
        "schemaVersion": 1,
        "task": "INTER_SYSTEM_TRANSIT_TO_STATION",
        "completed": True,
        "finalPhase": "VISUAL_CONFIRMATION_REQUIRED",
        "destinationSystem": destination_system,
        "destinationStation": destination_station,
        "systemTargetLocked": True,
        "stationTargetLocked": True,
        "hyperspaceChargingConfirmed": charging_seen,
        "hyperspaceTransitConfirmed": True,
        "cockpitReturnConfirmations": arrival_present,
        "supercruiseHudConfirmations": supercruise_confirmations,
        "destinationSystemTextConfirmations": system_confirmation["confirmations"],
        "initialAlignment": initial_alignment,
        "recoveryAlignment": recovery_alignment,
        "supercruiseCompleted": True,
        "dockingCompleted": True,
        "visualConfirmed": False,
        "sampleCount": sample,
    }
