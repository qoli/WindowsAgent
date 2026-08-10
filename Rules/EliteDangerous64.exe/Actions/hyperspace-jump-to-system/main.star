POLL_MS = 250
CHARGING_LIMIT = 160
TRANSIT_ABSENT_CONFIRMATIONS = 2
ARRIVAL_PRESENT_CONFIRMATIONS = 2
ARRIVAL_LIMIT = 240
SUPERCRUISE_HUD_CONFIRMATIONS = 2

def emit_update(phase, sample, target_system, child_action=None, hyperspace_state=None, flight_status=None, cockpit_hud=None, commanded_throttle=None, last_command=None, reason=None):
    stream.emit(
        type="action.hyperspace-jump-to-system.update",
        payload={
            "phase": phase,
            "sample": sample,
            "targetSystem": target_system,
            "childAction": child_action,
            "hyperspaceState": hyperspace_state,
            "flightStatus": flight_status,
            "cockpitHud": cockpit_hud,
            "commandedThrottle": commanded_throttle,
            "lastCommand": last_command,
            "reason": reason,
        },
    )

def coarse_align_target(target_system, sample, phase, control_profile):
    emit_update(phase, sample, target_system, child_action="elite-dangerous/align-station-target", commanded_throttle=0, reason="COMPASS_COARSE_ALIGNMENT")
    return action.call(id="elite-dangerous/align-station-target", inputs={"mode": "ALIGN", "stopBeforeAlign": True, "controlProfile": control_profile})

def align_target(target_system, sample, phase, control_profile):
    coarse = coarse_align_target(target_system, sample, phase, control_profile)
    initial_coarse_samples = coarse["sampleCount"]
    recovery_alignment = None
    escape_count = 0
    for _ in range(3):
        obstruction = action.call(id="elite-dangerous/hyperspace-target-occlusion", inputs={})["occlusion"]
        if obstruction["state"] != "CLEAR":
            if escape_count >= 2:
                fail("hyperspace target remained stellar-obstructed after two Supercruise escapes")
            emit_update("CLEARING_OCCLUSION", sample, target_system, child_action="elite-dangerous/clear-hyperspace-occlusion", commanded_throttle=0, reason=obstruction["state"] + ":CENTER=" + str(obstruction["centerCoverageRatio"]) + ":TOTAL=" + str(obstruction["stellarCoverageRatio"]))
            escape = action.call(id="elite-dangerous/clear-hyperspace-occlusion", inputs={"targetName": target_system})
            escape_count += 1
            coarse = coarse_align_target(target_system, sample, "REALIGNING_TARGET", "SUPERCRUISE_ASSIST")
            recovery_alignment = {"escapeCount": escape_count, "escape": escape, "coarseSamples": coarse["sampleCount"], "fineSamples": None}
            continue
        emit_update(phase, sample, target_system, child_action="elite-dangerous/align-visible-target", commanded_throttle=0, reason="VISIBLE_TARGET_FINE_ALIGNMENT")
        fine = action.call(id="elite-dangerous/align-visible-target", inputs={"targetName": target_system, "stopBeforeAlign": False})
        post_alignment = action.call(id="elite-dangerous/hyperspace-target-occlusion", inputs={})["occlusion"]
        if post_alignment["state"] == "CLEAR":
            if recovery_alignment != None:
                recovery_alignment = {"escapeCount": recovery_alignment["escapeCount"], "escape": recovery_alignment["escape"], "coarseSamples": recovery_alignment["coarseSamples"], "fineSamples": fine["sampleCount"]}
            return {
                "initialAlignment": {"coarseSamples": initial_coarse_samples, "fineSamples": fine["sampleCount"]},
                "recoveryAlignment": recovery_alignment,
            }
        if escape_count >= 2:
            fail("visible target alignment remained stellar-obstructed after two Supercruise escapes")
        emit_update("CLEARING_OCCLUSION", sample, target_system, child_action="elite-dangerous/clear-hyperspace-occlusion", commanded_throttle=0, reason="POST_ALIGNMENT_" + post_alignment["state"] + ":CENTER=" + str(post_alignment["centerCoverageRatio"]) + ":TOTAL=" + str(post_alignment["stellarCoverageRatio"]))
        escape = action.call(id="elite-dangerous/clear-hyperspace-occlusion", inputs={"targetName": target_system})
        escape_count += 1
        coarse = coarse_align_target(target_system, sample, "REALIGNING_TARGET", "SUPERCRUISE_ASSIST")
        recovery_alignment = {"escapeCount": escape_count, "escape": escape, "coarseSamples": coarse["sampleCount"], "fineSamples": None}
    fail("hyperspace target alignment did not reach a clear forward view")

def observe_hyperspace():
    observation = action.call(id="elite-dangerous/hyperspace-state", inputs={})["hyperspaceState"]
    return {
        "state": observation["state"],
        "flightStatus": observation["flightStatus"],
        "cockpitHud": observation["cockpitHud"]["state"],
        "promptText": observation["promptText"],
    }

def require_supercruise_hud(sample, target_system):
    confirmations = 0
    for _ in range(8):
        hud = action.call(id="elite-dangerous/supercruise-hud-state", inputs={})["supercruiseHud"]
        if hud["state"] == "ACTIVE":
            confirmations += 1
        else:
            confirmations = 0
        emit_update("CONFIRMING_SUPERCRUISE", sample, target_system, child_action="elite-dangerous/supercruise-hud-state", commanded_throttle=0, reason="SUPERCRUISE_HUD_" + hud["state"])
        if confirmations >= SUPERCRUISE_HUD_CONFIRMATIONS:
            return confirmations
        task.sleep(milliseconds=POLL_MS)
    fail("persistent Supercruise HUD was not confirmed after hyperspace cockpit return")

def journal_baseline():
    tail = action.call(id="elite-dangerous/filesystem/journal-navigation-tail", inputs={})
    latest = None
    for event in tail["events"]:
        timestamp = event["timestamp"]
        if timestamp != None and (latest == None or timestamp > latest):
            latest = timestamp
    return latest

def journal_jump_evidence(target_system, target_address, baseline):
    tail = action.call(id="elite-dangerous/filesystem/journal-navigation-tail", inputs={})
    start_jump = None
    fsd_jump = None
    for event in tail["events"]:
        timestamp = event["timestamp"]
        if timestamp == None or (baseline != None and timestamp <= baseline):
            continue
        if event["StarSystem"] != target_system:
            continue
        if target_address != None and event["SystemAddress"] != target_address:
            continue
        if event["event"] == "StartJump" and (start_jump == None or timestamp > start_jump):
            start_jump = timestamp
        if event["event"] == "FSDJump" and (fsd_jump == None or timestamp > fsd_jump):
            fsd_jump = timestamp
    return {"startJump": start_jump, "fsdJump": fsd_jump}

def completed_output(target_system, initial_alignment, recovery_alignment, sample, cockpit_confirmations, supercruise_confirmations, arrival_evidence):
    return {
        "schemaVersion": 1,
        "task": "HYPERSPACE_JUMP_TO_SYSTEM",
        "completed": True,
        "finalPhase": "ARRIVED_IN_SUPERCRUISE",
        "targetSystem": target_system,
        "targetLocked": True,
        "hyperspaceChargingConfirmed": True,
        "hyperspaceTransitConfirmed": True,
        "arrivalEvidence": arrival_evidence,
        "arrivalBrakeSent": True,
        "cockpitReturnConfirmations": cockpit_confirmations,
        "supercruiseHudConfirmations": supercruise_confirmations,
        "initialAlignment": initial_alignment,
        "recoveryAlignment": recovery_alignment,
        "sampleCount": sample,
    }

def main(ctx):
    target_system = ctx.inputs["targetSystem"]
    start_mode = ctx.inputs["startMode"]
    if start_mode == "NORMAL_SPACE" and not ctx.inputs["normalSpaceConfirmed"]:
        fail("normalSpaceConfirmed must be true for NORMAL_SPACE startMode")
    if start_mode == "SUPERCRUISE" and not ctx.inputs["supercruiseConfirmed"]:
        fail("supercruiseConfirmed must be true for SUPERCRUISE startMode")

    sample = 0
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    if ctx.inputs["targetLockConfirmed"]:
        stream.activity(message="Using caller-confirmed hyperspace target " + target_system, level="info")
        emit_update("TARGET_LOCKED", sample, target_system, commanded_throttle=0, reason="CALLER_CONFIRMED")
    else:
        stream.activity(message="Locking hyperspace target " + target_system, level="info")
        emit_update("LOCKING_TARGET", sample, target_system, child_action="elite-dangerous/select-and-lock-destination", commanded_throttle=0)
        target_lock = action.call(id="elite-dangerous/select-and-lock-destination", inputs={"targetName": target_system})
        if not target_lock["targetLocked"]:
            fail("hyperspace target lock Action did not confirm targetLocked")
        emit_update("TARGET_LOCKED", sample, target_system, child_action="elite-dangerous/select-and-lock-destination", commanded_throttle=0, reason=target_lock["result"])

    control_profile = "SUPERCRUISE_ASSIST" if start_mode == "SUPERCRUISE" else "NORMAL_SPACE"
    stream.activity(message="Aligning hyperspace target", level="info")
    alignment = align_target(target_system, sample, "ALIGNING_TARGET", control_profile)
    initial_alignment = alignment["initialAlignment"]
    recovery_alignment = alignment["recoveryAlignment"]
    initial = observe_hyperspace()
    if initial["cockpitHud"] != "PRESENT":
        fail("cockpit HUD was not present before hyperspace control")

    pre_jump_journal_timestamp = journal_baseline()
    jump = action.call(id="elite-dangerous/hyperspace-control", inputs={"command": "JUMP"})
    emit_update("AWAITING_FSD_CHARGING", sample, target_system, child_action="elite-dangerous/hyperspace-control", hyperspace_state=initial["state"], flight_status=initial["flightStatus"], cockpit_hud=initial["cockpitHud"], commanded_throttle=0, last_command="HYPERSPACE_JUMP", reason=jump["control"])

    charging_seen = False
    transit_absent = 0
    throttle_100_sent = False
    retry_sent = False
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
            emit_update("REALIGNING_TARGET", sample, target_system, hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=0, last_command="SET_THROTTLE_0", reason=throttle["control"])
            alignment = align_target(target_system, sample, "REALIGNING_TARGET", "SUPERCRUISE_ASSIST")
            recovery_alignment = alignment["recoveryAlignment"] if alignment["recoveryAlignment"] != None else alignment["initialAlignment"]
            throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
            throttle_100_sent = True
            command = "REALIGN_TARGETS+SET_THROTTLE_100"
            phase = "REALIGNING_TARGET"
            reason = throttle["control"]
        elif state == "COCKPIT_ABSENT":
            if not charging_seen:
                fail("cockpit HUD disappeared before FSD charging was visually confirmed")
            transit_absent += 1
            phase = "HYPERSPACE_TRANSIT_CANDIDATE"
        else:
            transit_absent = 0
        emit_update(phase, sample, target_system, hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=100 if throttle_100_sent else 0, last_command=command, reason=reason)
        journal = journal_jump_evidence(target_system, ctx.inputs.get("targetSystemAddress"), pre_jump_journal_timestamp) if sample % 4 == 0 else {"startJump": None, "fsdJump": None}
        if journal["startJump"] != None and not charging_seen:
            charging_seen = True
            phase = "FSD_START_CONFIRMED"
            reason = "JOURNAL_STARTJUMP:" + journal["startJump"]
            if not throttle_100_sent:
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
                throttle_100_sent = True
                command = "SET_THROTTLE_100"
            emit_update(phase, sample, target_system, child_action="elite-dangerous/filesystem/journal-navigation-tail", hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=100, last_command=command, reason=reason)
        if sample == 20 and not charging_seen and not retry_sent:
            retry = action.call(id="elite-dangerous/hyperspace-control", inputs={"command": "JUMP"})
            retry_sent = True
            emit_update("RETRYING_FSD_CONTROL", sample, target_system, child_action="elite-dangerous/hyperspace-control", hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=0, last_command="HYPERSPACE_JUMP_RETRY", reason=retry["control"])
        fsd_jump_timestamp = journal["fsdJump"]
        if fsd_jump_timestamp != None:
            throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            emit_update("ARRIVAL_BRAKE", sample, target_system, child_action="elite-dangerous/filesystem/journal-navigation-tail", hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=0, last_command="SET_THROTTLE_0", reason="JOURNAL_FSDJUMP:" + fsd_jump_timestamp)
            supercruise_confirmations = require_supercruise_hud(sample, target_system)
            action.clear_on_failure()
            stream.activity(message="Hyperspace arrival confirmed by Journal FSDJump", level="info")
            emit_update("ARRIVED_IN_SUPERCRUISE", sample, target_system, commanded_throttle=0, reason="JOURNAL_FSDJUMP+SUPERCRUISE_HUD")
            return completed_output(target_system, initial_alignment, recovery_alignment, sample, 0, supercruise_confirmations, "JOURNAL_FSDJUMP")
        if transit_absent >= TRANSIT_ABSENT_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if not charging_seen or transit_absent < TRANSIT_ABSENT_CONFIRMATIONS:
        fail("FSD charging followed by stable hyperspace cockpit absence was not confirmed")
    emit_update("HYPERSPACE_TRANSIT", sample, target_system, hyperspace_state="COCKPIT_ABSENT", cockpit_hud="ABSENT", commanded_throttle=100, reason="TWO_CONSECUTIVE_COCKPIT_ABSENT_SAMPLES")

    arrival_present = 0
    arrival_stop_sent = False
    for _ in range(ARRIVAL_LIMIT):
        sample += 1
        observation = observe_hyperspace()
        command = None
        phase = "AWAITING_COCKPIT_RETURN"
        if observation["cockpitHud"] == "PRESENT":
            arrival_present += 1
            if not arrival_stop_sent:
                throttle = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                arrival_stop_sent = True
                command = "SET_THROTTLE_0"
                phase = "ARRIVAL_BRAKE"
        else:
            arrival_present = 0
        emit_update(phase, sample, target_system, hyperspace_state=observation["state"], flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=0 if arrival_stop_sent else 100, last_command=command, reason=observation["promptText"])
        if arrival_present >= ARRIVAL_PRESENT_CONFIRMATIONS:
            break
        task.sleep(milliseconds=POLL_MS)
    if arrival_present < ARRIVAL_PRESENT_CONFIRMATIONS or not arrival_stop_sent:
        fail("stable cockpit return and first-frame arrival brake were not confirmed after hyperspace transit")

    supercruise_confirmations = require_supercruise_hud(sample, target_system)
    action.clear_on_failure()
    stream.activity(message="Hyperspace arrival confirmed", level="info")
    emit_update("ARRIVED_IN_SUPERCRUISE", sample, target_system, commanded_throttle=0, reason="COCKPIT_RETURN+SUPERCRUISE_HUD")
    return completed_output(target_system, initial_alignment, recovery_alignment, sample, arrival_present, supercruise_confirmations, "COCKPIT_TRANSITION")
