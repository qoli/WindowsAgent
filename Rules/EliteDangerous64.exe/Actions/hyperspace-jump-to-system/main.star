POLL_MS = 250
CHARGING_LIMIT = 160
TRANSIT_ABSENT_CONFIRMATIONS = 2
ARRIVAL_PRESENT_CONFIRMATIONS = 2
ARRIVAL_LIMIT = 240
SUPERCRUISE_HUD_CONFIRMATIONS = 2
MAX_TRANSIENT_WGC_OBSERVATION_ERRORS = 5
POST_ALIGNMENT_HEAT_LIMIT = 60
POST_ALIGNMENT_HEAT_CONFIRMATIONS = 3
POST_ALIGNMENT_HEAT_SAMPLES = 12
POST_ALIGNMENT_BLOCKING_STELLAR_RATIO = 0.08
POST_ALIGNMENT_BLOCKING_CENTER_RATIO = 0.10

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
    result = action.call(id="elite-dangerous/align-station-target", inputs={"mode": "ALIGN", "targetMotion": "STATIC", "alignmentPurpose": "HYPERSPACE_CHARGE", "stopBeforeAlign": True, "controlProfile": control_profile})
    target = result["finalObservation"]["target"]
    maximum_handoff_distance = 6.0 if control_profile == "SUPERCRUISE_ASSIST" else 12.0
    if not target["detected"] or target["presentation"] != "SOLID" or target["centerDistancePixels"] > maximum_handoff_distance or result["stableConfirmations"] < 3:
        fail("pre-FSD Compass handoff did not satisfy the profile-specific three-frame center Gate")
    return result

def post_alignment_heat_gate(target_system, sample):
    confirmations = 0
    last_percent = None
    for _ in range(POST_ALIGNMENT_HEAT_SAMPLES):
        heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
        last_percent = heat["percent"]
        if heat["state"] == "KNOWN" and last_percent <= POST_ALIGNMENT_HEAT_LIMIT:
            confirmations += 1
        else:
            confirmations = 0
        reason = "HEAT_UNKNOWN"
        if heat["state"] == "KNOWN":
            reason = "KNOWN_" + str(last_percent) + "_OF_" + str(POST_ALIGNMENT_HEAT_LIMIT)
        emit_update("VERIFYING_POST_ALIGNMENT_HEAT", sample, target_system, child_action="elite-dangerous/ship-heat", commanded_throttle=0, reason=reason)
        if confirmations >= POST_ALIGNMENT_HEAT_CONFIRMATIONS:
            return {"percent": last_percent, "confirmations": confirmations}
        task.sleep(milliseconds=POLL_MS)
    fail("post-alignment ship heat did not produce three known observations at or below 60 percent")

def substantial_post_alignment_obstruction(obstruction):
    return (
        obstruction["state"] == "BLOCKING" or
        obstruction["stellarCoverageRatio"] >= POST_ALIGNMENT_BLOCKING_STELLAR_RATIO or
        obstruction["centerCoverageRatio"] >= POST_ALIGNMENT_BLOCKING_CENTER_RATIO
    )

def align_target(target_system, sample, phase, control_profile, start_mode):
    escape_count = 0
    latest_escape = None
    for _ in range(3):
        obstruction = action.call(id="elite-dangerous/hyperspace-target-occlusion", inputs={})["occlusion"]
        if not obstruction["safeToCharge"]:
            if escape_count >= 2:
                fail("hyperspace target remained stellar-obstructed after two Supercruise escapes")
            emit_update("CLEARING_OCCLUSION", sample, target_system, child_action="elite-dangerous/clear-hyperspace-occlusion", commanded_throttle=0, reason="UNSAFE_TO_CHARGE:" + obstruction["state"] + ":CENTER=" + str(obstruction["centerCoverageRatio"]) + ":TOTAL=" + str(obstruction["stellarCoverageRatio"]) + ":MAX_CELL=" + str(obstruction["maximumCellCoverageRatio"]))
            latest_escape = action.call(id="elite-dangerous/clear-hyperspace-occlusion", inputs={"targetName": target_system, "startMode": start_mode})
            escape_count += 1
            continue
        alignment_phase = "REALIGNING_TARGET" if escape_count > 0 else phase
        alignment_profile = "SUPERCRUISE_ASSIST" if escape_count > 0 else control_profile
        coarse = coarse_align_target(target_system, sample, alignment_phase, alignment_profile)
        # Compass alignment changes the forward view. Re-check the stellar
        # field before visible-target refinement: when the destination lies
        # behind the arrival star, the reticle can be washed out and the fine
        # controller would otherwise steer deeper into the obstruction. The
        # clearance flight must create enough parallax before either fine
        # alignment or FSD is allowed.
        coarse_obstruction = action.call(id="elite-dangerous/hyperspace-target-occlusion", inputs={})["occlusion"]
        emit_update("VERIFYING_COMPASS_ALIGNED_OCCLUSION", sample, target_system, child_action="elite-dangerous/hyperspace-target-occlusion", commanded_throttle=0, reason=coarse_obstruction["state"] + ":CENTER=" + str(coarse_obstruction["centerCoverageRatio"]) + ":TOTAL=" + str(coarse_obstruction["stellarCoverageRatio"]))
        if substantial_post_alignment_obstruction(coarse_obstruction):
            if escape_count >= 2:
                fail("Compass-aligned hyperspace target remained substantially stellar-obstructed after two Supercruise escapes")
            emit_update("CLEARING_OCCLUSION", sample, target_system, child_action="elite-dangerous/clear-hyperspace-occlusion", commanded_throttle=0, reason="COMPASS_ALIGNED_STELLAR_OBSTRUCTION")
            latest_escape = action.call(id="elite-dangerous/clear-hyperspace-occlusion", inputs={"targetName": target_system, "startMode": "SUPERCRUISE"})
            escape_count += 1
            continue
        emit_update(phase, sample, target_system, child_action="elite-dangerous/align-visible-target", commanded_throttle=0, reason="VISIBLE_TARGET_FINE_ALIGNMENT")
        fine_attempt = action.try_call(id="elite-dangerous/align-visible-target", inputs={"targetName": target_system, "stopBeforeAlign": False, "centerHintConfirmed": True})
        if not fine_attempt["ok"]:
            fail(fine_attempt["error"])
        fine = fine_attempt["output"]
        # The centered destination reticle can contaminate a strict orange-ratio
        # safeToCharge bit, but cannot create the substantial topology below. If
        # alignment points the ship back into the arrival star, clear again and
        # repeat both alignment children before allowing FSD control.
        post_obstruction = action.call(id="elite-dangerous/hyperspace-target-occlusion", inputs={})["occlusion"]
        emit_update("VERIFYING_POST_ALIGNMENT_OCCLUSION", sample, target_system, child_action="elite-dangerous/hyperspace-target-occlusion", commanded_throttle=0, reason=post_obstruction["state"] + ":CENTER=" + str(post_obstruction["centerCoverageRatio"]) + ":TOTAL=" + str(post_obstruction["stellarCoverageRatio"]))
        if substantial_post_alignment_obstruction(post_obstruction):
            if escape_count >= 2:
                fail("aligned hyperspace target remained substantially stellar-obstructed after two Supercruise escapes")
            emit_update("CLEARING_OCCLUSION", sample, target_system, child_action="elite-dangerous/clear-hyperspace-occlusion", commanded_throttle=0, reason="POST_ALIGNMENT_STELLAR_OBSTRUCTION")
            latest_escape = action.call(id="elite-dangerous/clear-hyperspace-occlusion", inputs={"targetName": target_system, "startMode": "SUPERCRUISE"})
            escape_count += 1
            continue
        heat = post_alignment_heat_gate(target_system, sample)
        recovery_alignment = None
        if escape_count > 0:
            recovery_alignment = {"escapeCount": escape_count, "escape": latest_escape, "coarseSamples": coarse["sampleCount"], "fineSamples": fine["sampleCount"]}
        return {
            "initialAlignment": {"coarseSamples": coarse["sampleCount"], "fineSamples": fine["sampleCount"], "postAlignmentHeatPercent": heat["percent"], "postAlignmentHeatConfirmations": heat["confirmations"]},
            "recoveryAlignment": recovery_alignment,
        }
    fail("hyperspace target alignment did not reach a clear forward view")

def observe_hyperspace():
    observation = action.call(id="elite-dangerous/hyperspace-state", inputs={})["hyperspaceState"]
    return {
        "state": observation["state"],
        "flightStatus": observation["flightStatus"],
        "cockpitHud": observation["cockpitHud"]["state"],
        "promptText": observation["promptText"],
    }

def transient_wgc_region_capture_error(text):
    return "persistent WGC worker region capture" in text and "persistent region capture failed" in text

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

def same_system_name(actual, expected):
    if actual == None or expected == None:
        return False
    # Journal uses canonical title casing while callers commonly preserve the
    # all-caps HUD spelling. Identity stays otherwise exact.
    return actual.upper() == expected.upper()

def journal_jump_evidence(target_system, target_address, baseline):
    tail = action.call(id="elite-dangerous/filesystem/journal-navigation-tail", inputs={})
    start_jump = None
    fsd_jump = None
    for event in tail["events"]:
        timestamp = event["timestamp"]
        if timestamp == None or (baseline != None and timestamp <= baseline):
            continue
        if not same_system_name(event["StarSystem"], target_system):
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
    alignment = align_target(target_system, sample, "ALIGNING_TARGET", control_profile, start_mode)
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
    transient_wgc_errors = 0
    for _ in range(CHARGING_LIMIT):
        sample += 1
        observation_attempt = action.try_call(id="elite-dangerous/hyperspace-state", inputs={})
        if not observation_attempt["ok"]:
            error_text = observation_attempt["error"]
            if not transient_wgc_region_capture_error(error_text):
                fail("hyperspace visual observation failed: " + error_text)
            transient_wgc_errors += 1
            phase = "FSD_CHARGING" if charging_seen else "AWAITING_FSD_CHARGING"
            emit_update(phase, sample, target_system, child_action="elite-dangerous/hyperspace-state", commanded_throttle=100 if throttle_100_sent else 0, reason="TRANSIENT_WGC_OBSERVATION_SKIPPED_" + str(transient_wgc_errors))
            if transient_wgc_errors > MAX_TRANSIENT_WGC_OBSERVATION_ERRORS:
                fail("hyperspace visual observation exceeded five transient WGC region-capture errors: " + error_text)
            task.sleep(milliseconds=POLL_MS)
            continue
        observation_raw = observation_attempt["output"]["hyperspaceState"]
        observation = {
            "state": observation_raw["state"],
            "flightStatus": observation_raw["flightStatus"],
            "cockpitHud": observation_raw["cockpitHud"]["state"],
            "promptText": observation_raw["promptText"],
        }
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
            cancel = action.call(id="elite-dangerous/hyperspace-control", inputs={"command": "JUMP"})
            emit_update("CANCELLING_MISALIGNED_FSD", sample, target_system, hyperspace_state=state, flight_status=observation["flightStatus"], cockpit_hud=observation["cockpitHud"], commanded_throttle=0, last_command="SET_THROTTLE_0+HYPERSPACE_JUMP_CANCEL", reason=cancel["control"])
            fail("game reported ALIGNMENT_REQUIRED after strict pre-FSD alignment; charge cancelled")
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
        observation_attempt = action.try_call(id="elite-dangerous/hyperspace-state", inputs={})
        if not observation_attempt["ok"]:
            error_text = observation_attempt["error"]
            if not transient_wgc_region_capture_error(error_text):
                fail("hyperspace arrival visual observation failed: " + error_text)
            transient_wgc_errors += 1
            emit_update("AWAITING_COCKPIT_RETURN", sample, target_system, child_action="elite-dangerous/hyperspace-state", commanded_throttle=0 if arrival_stop_sent else 100, reason="TRANSIENT_WGC_OBSERVATION_SKIPPED_" + str(transient_wgc_errors))
            if transient_wgc_errors > MAX_TRANSIENT_WGC_OBSERVATION_ERRORS:
                fail("hyperspace arrival observation exceeded five transient WGC region-capture errors: " + error_text)
            task.sleep(milliseconds=POLL_MS)
            continue
        observation_raw = observation_attempt["output"]["hyperspaceState"]
        observation = {
            "state": observation_raw["state"],
            "flightStatus": observation_raw["flightStatus"],
            "cockpitHud": observation_raw["cockpitHud"]["state"],
            "promptText": observation_raw["promptText"],
        }
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
