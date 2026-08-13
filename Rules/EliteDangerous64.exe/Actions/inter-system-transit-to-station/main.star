POLL_MS = 250

def same_system_name(actual, expected):
    if actual == None or expected == None:
        return False
    return actual.upper() == expected.upper()

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

def require_arrived_supercruise_resume(target_name):
    tail = action.call(id="elite-dangerous/filesystem/journal-navigation-tail", inputs={})
    latest = None
    for event in tail["events"]:
        if event["event"] == "FSDJump" and (latest == None or event["timestamp"] > latest["timestamp"]):
            latest = event
    if latest == None or not same_system_name(latest["StarSystem"], target_name):
        fail("latest Journal FSDJump does not match destinationSystem for arrived Supercruise resume")
    confirmations = 0
    for _ in range(8):
        hud = action.call(id="elite-dangerous/supercruise-hud-state", inputs={})["supercruiseHud"]
        if hud["state"] == "ACTIVE":
            confirmations += 1
        else:
            confirmations = 0
        emit_update("CONFIRMING_DESTINATION_SYSTEM", 0, target_name=target_name, child_action="elite-dangerous/supercruise-hud-state", commanded_throttle=0, reason="RESUME_FSDJUMP:" + latest["timestamp"] + ":HUD=" + hud["state"])
        if confirmations >= 2:
            return {"event": latest, "supercruiseHudConfirmations": confirmations}
        task.sleep(milliseconds=POLL_MS)
    fail("arrived Supercruise resume did not confirm persistent Supercruise HUD")

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
    if start_mode == "SUPERCRUISE" and not ctx.inputs["supercruiseConfirmed"]:
        fail("supercruiseConfirmed must be true for SUPERCRUISE startMode")
    if start_mode == "ARRIVED_SUPERCRUISE" and not ctx.inputs["supercruiseConfirmed"]:
        fail("supercruiseConfirmed must be true for ARRIVED_SUPERCRUISE startMode")

    sample = 0
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    if start_mode == "DOCKED":
        stream.activity(message="Leaving origin station", level="info")
        emit_update("DEPARTURE", sample, child_action="elite-dangerous/leave-station", reason="AWAITING_CHILD_WORKFLOW")
        action.call(id="elite-dangerous/leave-station", inputs={"stationConfirmed": True})
        emit_update("DEPARTURE_COMPLETED", sample, child_action="elite-dangerous/leave-station", commanded_throttle=0, reason="NORMAL_SPACE_STOP_CONFIRMED")
        jump_start_mode = "NORMAL_SPACE"
    else:
        jump_start_mode = start_mode

    if start_mode == "ARRIVED_SUPERCRUISE":
        resume = require_arrived_supercruise_resume(destination_system)
        route = {"routeId": "RESUME:" + resume["event"]["timestamp"]}
        jump = {
            "hyperspaceChargingConfirmed": True,
            "hyperspaceTransitConfirmed": True,
            "arrivalEvidence": "JOURNAL_FSDJUMP_RESUME",
            "cockpitReturnConfirmations": 0,
            "supercruiseHudConfirmations": resume["supercruiseHudConfirmations"],
            "initialAlignment": {},
            "recoveryAlignment": None,
        }
        emit_update("SYSTEM_LOCKED", sample, target_name=destination_system, child_action="elite-dangerous/filesystem/journal-navigation-tail", commanded_throttle=0, reason="RESUMED_MATCHING_FSDJUMP")
    else:
        stream.activity(message="Plotting exact one-hop route", level="info")
        emit_update("PLOTTING_ROUTE", sample, target_name=destination_system, child_action="elite-dangerous/plot-route-to-system", commanded_throttle=0)
        route = action.call(id="elite-dangerous/plot-route-to-system", inputs={"targetSystem": destination_system, "maxJumps": 1})
        if not route["completed"] or route["jumpCount"] != 1:
            fail("plot-route-to-system did not establish one exact hop")
        emit_update("ROUTE_READY", sample, target_name=destination_system, child_action="elite-dangerous/plot-route-to-system", commanded_throttle=0, reason=route["result"] + ":" + route["routeId"])

        stream.activity(message="Starting one-System hyperspace jump", level="info")
        emit_update("LOCKING_SYSTEM", sample, target_name=destination_system, child_action="elite-dangerous/hyperspace-jump-to-system", commanded_throttle=0, reason="PLOTTED_ROUTE_CONFIRMED")
        jump = action.call(id="elite-dangerous/hyperspace-jump-to-system", inputs={
            "targetSystem": destination_system,
            "targetSystemAddress": None,
            "targetLockConfirmed": True,
            "startMode": jump_start_mode,
            "normalSpaceConfirmed": jump_start_mode == "NORMAL_SPACE",
            "supercruiseConfirmed": jump_start_mode == "SUPERCRUISE",
        })
        if not jump["completed"] or jump["finalPhase"] != "ARRIVED_IN_SUPERCRUISE" or not jump["arrivalBrakeSent"]:
            fail("hyperspace-jump-to-system did not reach a stopped Supercruise arrival")
        sample = jump["sampleCount"]
        emit_update("SYSTEM_LOCKED", sample, target_name=destination_system, child_action="elite-dangerous/hyperspace-jump-to-system", commanded_throttle=0, reason="TARGET_LOCK_CONFIRMED_BY_CHILD")
        emit_update("FSD_CHARGING", sample, target_name=destination_system, child_action="elite-dangerous/hyperspace-jump-to-system", hyperspace_state="FSD_CHARGING", flight_status="FSD_CHARGING", cockpit_hud="PRESENT", commanded_throttle=0, reason="CHILD_CONFIRMED")
        emit_update("HYPERSPACE_TRANSIT", sample, target_name=destination_system, child_action="elite-dangerous/hyperspace-jump-to-system", hyperspace_state="COCKPIT_ABSENT", cockpit_hud="ABSENT", commanded_throttle=0, reason="CHILD_CONFIRMED")

    stream.activity(message="Destination system arrival confirmed", level="info")
    emit_update("DESTINATION_SYSTEM_CONFIRMED", sample, target_name=destination_system, child_action="elite-dangerous/hyperspace-jump-to-system", commanded_throttle=0, reason=jump["arrivalEvidence"])

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
        "routeId": route["routeId"],
        "systemTargetLocked": True,
        "stationTargetLocked": True,
        "hyperspaceChargingConfirmed": jump["hyperspaceChargingConfirmed"],
        "hyperspaceTransitConfirmed": jump["hyperspaceTransitConfirmed"],
        "cockpitReturnConfirmations": jump["cockpitReturnConfirmations"],
        "supercruiseHudConfirmations": jump["supercruiseHudConfirmations"],
        "destinationSystemArrivalEvidence": jump["arrivalEvidence"],
        "initialAlignment": jump["initialAlignment"],
        "recoveryAlignment": jump["recoveryAlignment"],
        "supercruiseCompleted": True,
        "dockingCompleted": True,
        "visualConfirmed": False,
        "sampleCount": sample,
    }
