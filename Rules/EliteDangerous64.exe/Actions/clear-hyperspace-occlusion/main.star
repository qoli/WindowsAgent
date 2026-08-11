MAX_CHARGE_START_HEAT_PERCENT = 60
CHARGE_HEAT_CANCEL_PERCENT = 75
# Once the visible Escape Vector has been aligned, the game owns the short FSD
# entry countdown.  The heat-wall effect can obscure the reticle during that
# countdown, so the Action no longer treats either reticle visibility or the
# generic OverHeating flag as a reason to abort.  Visual heat remains the
# authoritative bounded gate for this phase.
POST_ALIGNMENT_COUNTDOWN_HEAT_CANCEL_PERCENT = 160
HEAT_CONFIRMATIONS_REQUIRED = 3
COOLING_SAMPLE_LIMIT = 30
STATUS_OBSERVER_RETRY_LIMIT = 3
ESCAPE_VECTOR_SAMPLE_LIMIT = 60
ESCAPE_VECTOR_MISSING_LIMIT = 8
ESCAPE_VECTOR_UNKNOWN_LIMIT = 8
ESCAPE_VECTOR_CENTER_RADIUS_PIXELS = 4.0
ESCAPE_VECTOR_CENTER_CONFIRMATIONS_REQUIRED = 1
COARSE_ALIGNMENT_HOLD_MS = 600
MEDIUM_ALIGNMENT_HOLD_MS = 300
FINE_ALIGNMENT_HOLD_MS = 120
TRANSITION_BRAKE_MS = 300
CHARGE_HEAT_SAMPLE_CYCLES = 1
CHARGE_HEAT_UNKNOWN_LIMIT = 3
HEAT_VIEW_SETTLE_MS = 300
ALIGNMENT_WORSENING_PIXELS = 2.0
PREALIGN_PROBE_LIMIT = 8
PREALIGN_STATUS_SAMPLE_LIMIT = 40
PREALIGN_OWNERSHIP_SAMPLE_LIMIT = 16
# The charging Compass flashes. A 100 ms cadence phase-locked with the live
# blink and produced an entire false-missing burst after one pulse. 137 ms
# deliberately walks across that phase; sixteen samples also cover a delayed
# post-toggle HUD presentation without extending a probe beyond ~2.2 seconds.
PREALIGN_COMPASS_SAMPLE_INTERVAL_MS = 137
PREALIGN_HEAT_CANCEL_PERCENT = 70
PREALIGN_COMPASS_SWITCH_PIXELS = 8.0
HOLLOW_PREALIGN_HOLD_MS = 3000
PREALIGN_FAR_HOLD_MS = 3000
PREALIGN_MEDIUM_HOLD_MS = 1800
PREALIGN_NEAR_HOLD_MS = 700
PREALIGN_FINE_HOLD_MS = 200
SUPERCRUISE_CANCEL_LIMIT = 40
SUPERCRUISE_ESCAPE_DURATION_MS = 30000

def empty_target():
    return {"detected": None, "presentation": None, "offsetX": None, "offsetY": None, "centerDistancePixels": None, "centerZone": {"inside": None}}

def emit_update(phase, target_name, sample, turn_count, observation=None, target=None, selected_control=None, command_hold_ms=None, probe_delta=None, stable_heading_confirmations=0, alignment_confirmations=0, throttle=0, mass_lock=None, heat_percent=None, fsd_charging=None, fsd_hyperdrive_charging=None, fsd_cooldown=None, over_heating=None, supercruise=None, elapsed_ms=0, reason=None, escape_vector_evidence_state=None):
    compass_target = empty_target() if target == None else target
    if escape_vector_evidence_state == None:
        escape_vector_evidence_state = "LIVE_CHARGE" if fsd_charging == True and compass_target["detected"] == True else "NONE"
    stream.emit(type="action.clear-hyperspace-occlusion.update", payload={
        "phase": phase,
        "targetName": target_name,
        "sample": sample,
        "turnCount": turn_count,
        "occlusionState": None if observation == None else observation["state"],
        "stellarCoverageRatio": None if observation == None else observation["stellarCoverageRatio"],
        "centerCoverageRatio": None if observation == None else observation["centerCoverageRatio"],
        "maximumCellCoverageRatio": None if observation == None else observation["maximumCellCoverageRatio"],
        "safeToCharge": None if observation == None else observation["safeToCharge"],
        "directionConfidence": None if observation == None else observation["directionConfidence"],
        "recommendedControl": None if observation == None else observation["recommendedControl"],
        "selectedControl": selected_control,
        "commandHoldMs": command_hold_ms,
        "probeDelta": probe_delta,
        "stableHeadingConfirmations": stable_heading_confirmations,
        "escapeVectorDetected": compass_target["detected"],
        "escapeVectorPresentation": compass_target["presentation"],
        "escapeVectorOffsetX": compass_target["offsetX"],
        "escapeVectorOffsetY": compass_target["offsetY"],
        "escapeVectorCenterDistancePixels": compass_target["centerDistancePixels"],
        "escapeVectorInsideCenterZone": compass_target["centerZone"]["inside"],
        "escapeVectorEvidenceState": escape_vector_evidence_state,
        "escapeVectorAlignmentConfirmations": alignment_confirmations,
        "commandedThrottle": throttle,
        "massLock": mass_lock,
        "heatPercent": heat_percent,
        "fsdCharging": fsd_charging,
        "fsdHyperdriveCharging": fsd_hyperdrive_charging,
        "fsdCooldown": fsd_cooldown,
        "overHeating": over_heating,
        "supercruise": supercruise,
        "elapsedMs": elapsed_ms,
        "reason": reason,
    })

def observe_obstruction():
    return action.call(id="elite-dangerous/hyperspace-target-occlusion", inputs={})["occlusion"]

def has_flag(value, bit_value):
    return (value // bit_value) % 2 == 1

def observe_status_flags():
    last_error = None
    for attempt_index in range(STATUS_OBSERVER_RETRY_LIMIT):
        attempt = action.try_call(id="elite-dangerous/filesystem/status", inputs={})
        if attempt["ok"]:
            status = attempt["output"]
            if status["state"] != "AVAILABLE":
                fail("Status.json evidence is required for FSD safety")
            data = status["data"]
            if "Flags" not in data or "Flags2" not in data:
                fail("Status.json Flags and Flags2 are required for FSD safety")
            flags = data["Flags"]
            flags2 = data["Flags2"]
            return {
                "supercruise": has_flag(flags, 16),
                "massLock": has_flag(flags, 65536),
                "fsdCharging": has_flag(flags, 131072),
                "fsdCooldown": has_flag(flags, 262144),
                "overHeating": has_flag(flags, 1048576),
                "fsdHyperdriveCharging": has_flag(flags2, 524288),
                "freshness": status["freshness"],
                "sourceTimestamp": status["source"]["sourceTimestamp"],
            }
        last_error = attempt["error"]
        if attempt_index + 1 < STATUS_OBSERVER_RETRY_LIMIT:
            task.sleep(milliseconds=250)
    fail("Status.json observer failed after three bounded attempts: " + last_error)

def opposite(control):
    if control == "PITCH_UP":
        return "PITCH_DOWN"
    if control == "PITCH_DOWN":
        return "PITCH_UP"
    if control == "YAW_LEFT":
        return "YAW_RIGHT"
    return "YAW_LEFT"

def is_safe_initial_heading(observation):
    return observation["safeToCharge"]

def choose_escape_vector_control(target):
    offset_x = target["offsetX"]
    offset_y = target["offsetY"]
    if abs(offset_x) >= abs(offset_y) and offset_x != 0:
        return "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
    if offset_y != 0:
        return "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
    return None

def choose_alignment_hold(target):
    distance = target["centerDistancePixels"]
    if target["presentation"] == "HOLLOW" or distance > 40:
        return COARSE_ALIGNMENT_HOLD_MS
    if distance > 16:
        return MEDIUM_ALIGNMENT_HOLD_MS
    return FINE_ALIGNMENT_HOLD_MS

def choose_prealignment_hold(target):
    # A HOLLOW marker is an antipodal projection: its small signed offset does
    # not describe a trustworthy screen-space correction.  Reuse the proven
    # align-station-target topology rule and make a bounded coarse turn in one
    # fixed direction until the marker becomes SOLID.
    if target["presentation"] == "HOLLOW":
        return HOLLOW_PREALIGN_HOLD_MS
    distance = target["centerDistancePixels"]
    if distance > 40:
        return PREALIGN_FAR_HOLD_MS
    if distance > 16:
        return PREALIGN_MEDIUM_HOLD_MS
    if distance > 6:
        return PREALIGN_NEAR_HOLD_MS
    return PREALIGN_FINE_HOLD_MS

def choose_sustained_escape_control(target):
    if target["presentation"] == "HOLLOW":
        return choose_escape_vector_control(target)
    if target["presentation"] == "SOLID" and target["centerDistancePixels"] > 40:
        return choose_escape_vector_control(target)
    return None

def reset_failure_compensation(active_control=None, active_lease_id=None, cancel_charge=False):
    action.clear_on_failure()
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    if cancel_charge:
        action.on_failure(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"}, critical=True, timeout_milliseconds=2000)
    if active_lease_id != None:
        action.on_failure(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "STOP", "control": active_control, "leaseId": active_lease_id}, critical=True, timeout_milliseconds=2000)

def release_escape_hold(active_control, active_lease_id, cancel_charge=False):
    if active_lease_id == None:
        return
    action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "STOP", "control": active_control, "leaseId": active_lease_id})
    reset_failure_compensation(cancel_charge=cancel_charge)

def run_prealignment_segment(control, duration_ms):
    hold_result = action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "START", "control": control})
    lease_id = hold_result["leaseId"]
    reset_failure_compensation(control, lease_id, cancel_charge=False)
    remaining_ms = duration_ms
    while remaining_ms > 0:
        sleep_ms = 1000 if remaining_ms > 1000 else remaining_ms
        task.sleep(milliseconds=sleep_ms)
        remaining_ms -= sleep_ms
    action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "STOP", "control": control, "leaseId": lease_id})
    reset_failure_compensation()

def cancel_supercruise_charge(target_name, sample, turn_count, observation, target, selected_control, reason, pre_cancel_timestamp, preserve_snapshot=False):
    # This function owns the single explicit cancel command. Remove the generic
    # charge-phase toggle compensation first so a later observer failure cannot
    # issue a second toggle and accidentally restart charging.
    reset_failure_compensation()
    action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    for _ in range(SUPERCRUISE_CANCEL_LIMIT):
        task.sleep(milliseconds=250)
        sample += 1
        status = observe_status_flags()
        current_after_command = status["sourceTimestamp"] != pre_cancel_timestamp
        has_snapshot = target != None and target["detected"] == True
        evidence_state = "LIVE_CHARGE" if status["fsdCharging"] and has_snapshot else ("CACHED_ONE_SHOT" if preserve_snapshot and has_snapshot else "EXPIRED")
        emit_update("CANCELLING_CHARGE", target_name, sample, turn_count, observation=observation, target=target, selected_control=selected_control, throttle=0, mass_lock="ON" if status["massLock"] else "OFF", fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], reason=reason if current_after_command else "WAITING_POST_CANCEL_STATUS", escape_vector_evidence_state=evidence_state)
        if current_after_command and not status["fsdCharging"] and not status["fsdHyperdriveCharging"]:
            reset_failure_compensation()
            return sample
    fail("FSD cancellation did not produce a newer non-charging Status snapshot within ten seconds")

def wait_for_safe_heat(target_name, sample, turn_count, observation, selected_control, stable_heading_confirmations):
    heat_confirmations = 0
    heat_percent = None
    for _ in range(COOLING_SAMPLE_LIMIT):
        heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
        heat_percent = heat["percent"]
        if heat["state"] == "KNOWN":
            heat_confirmations = heat_confirmations + 1 if heat_percent <= MAX_CHARGE_START_HEAT_PERCENT else 0
        sample += 1
        emit_update("COOLING_BEFORE_CHARGE", target_name, sample, turn_count, observation=observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, heat_percent=heat_percent, mass_lock="OFF", reason="MAX_START_HEAT:" + str(MAX_CHARGE_START_HEAT_PERCENT))
        if heat_confirmations >= HEAT_CONFIRMATIONS_REQUIRED:
            return {"sample": sample, "heatPercent": heat_percent}
        task.sleep(milliseconds=500)
    fail("ship heat did not produce three known observations at or below the charge-start limit")

def main(ctx):
    target_name = ctx.inputs["targetName"]
    reset_failure_compensation()
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    sample = 1
    turn_count = 0
    initial = observe_obstruction()
    emit_update("OBSERVING_FORWARD_VIEW", target_name, sample, turn_count, observation=initial, reason="DIAGNOSTIC_ONLY_ESCAPE_VECTOR_OWNS_ANGLE")
    selected_control = None
    final_observation = initial
    stable_heading_confirmations = 0

    ship = action.call(id="elite-dangerous/ship-status", inputs={})["shipStatus"]
    if ship["landingGear"]["state"] != "OFF" or ship["cargoScoop"]["state"] != "OFF":
        fail("stellar escape requires visually confirmed Landing Gear and Cargo Scoop OFF")

    # Status.json is an event-state file, not a heartbeat. Its latest AVAILABLE
    # snapshot is the preflight baseline; after issuing Supercruise, a changed
    # source timestamp is required before charge flags may drive the workflow.
    status = observe_status_flags()
    sample += 1
    if status["massLock"] or status["overHeating"] or status["fsdCharging"] or status["fsdCooldown"] or status["supercruise"]:
        fail("Supercruise stellar-escape preflight requires current normal-space idle Status with Mass Lock OFF")

    heat_result = wait_for_safe_heat(target_name, sample, turn_count, final_observation, selected_control, stable_heading_confirmations)
    sample = heat_result["sample"]
    charge_start_heat_percent = heat_result["heatPercent"]

    # Escape Vector exists only while Supercruise is charging. Discover and
    # align it through short probe charges that are cancelled before turning;
    # all potentially long attitude work therefore happens at 0% throttle and
    # without an active heat-producing FSD charge.
    prealigned = False
    visible_alignment_completed = False
    visible_alignment_confirmations = 0
    final_target = empty_target()
    prealign_hollow_control = None
    prealign_previous_presentation = None
    prealign_previous_distance = None
    prealign_last_control = None
    for _ in range(PREALIGN_PROBE_LIMIT):
        baseline_attempt = action.try_call(id="elite-dangerous/compass", inputs={})
        baseline_target = empty_target()
        baseline_detected = False
        if baseline_attempt["ok"] and baseline_attempt["output"]["target"]["detected"]:
            baseline_target = baseline_attempt["output"]["target"]
            baseline_detected = True
        probe_precharge_timestamp = status["sourceTimestamp"]
        action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
        reset_failure_compensation(cancel_charge=True)
        probe_status = status
        probe_charging = False
        for _ in range(PREALIGN_STATUS_SAMPLE_LIMIT):
            task.sleep(milliseconds=50)
            sample += 1
            probe_status = observe_status_flags()
            if probe_status["sourceTimestamp"] != probe_precharge_timestamp and probe_status["fsdCharging"]:
                probe_charging = True
                break
        if not probe_charging:
            fail("Supercruise prealignment probe did not produce a newer charging Status snapshot")
        if probe_status["massLock"] or probe_status["overHeating"] or probe_status["fsdHyperdriveCharging"]:
            cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "PREALIGN_STATUS_SAFETY_GATE", probe_status["sourceTimestamp"])
            fail("Supercruise prealignment probe crossed a Status safety gate")
        ownership_confirmed = False
        solid_votes = 0
        hollow_votes = 0
        solid_target = None
        hollow_target = None
        for _ in range(PREALIGN_OWNERSHIP_SAMPLE_LIMIT):
            task.sleep(milliseconds=PREALIGN_COMPASS_SAMPLE_INTERVAL_MS)
            candidate_attempt = action.try_call(id="elite-dangerous/compass", inputs={})
            sample += 1
            if not candidate_attempt["ok"]:
                emit_update("PROBING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=empty_target(), selected_control=None, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=True, supercruise=False, reason="COMPASS_SAMPLE_UNAVAILABLE:" + str(candidate_attempt["errorCode"]))
                continue
            if not candidate_attempt["output"]["target"]["detected"]:
                emit_update("PROBING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=empty_target(), selected_control=None, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=True, supercruise=False, reason="COMPASS_TARGET_NOT_DETECTED")
                continue
            candidate_target = candidate_attempt["output"]["target"]
            switch_delta = 200.0 if not baseline_detected else float(abs(candidate_target["offsetX"] - baseline_target["offsetX"]) + abs(candidate_target["offsetY"] - baseline_target["offsetY"]))
            presentation_changed = not baseline_detected or candidate_target["presentation"] != baseline_target["presentation"]
            if presentation_changed or switch_delta >= PREALIGN_COMPASS_SWITCH_PIXELS:
                ownership_confirmed = True
                if candidate_target["presentation"] == "SOLID":
                    solid_votes += 1
                    solid_target = candidate_target
                elif candidate_target["presentation"] == "HOLLOW":
                    hollow_votes += 1
                    hollow_target = candidate_target
            emit_update("PROBING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=candidate_target, selected_control=None, probe_delta=min(1.0, switch_delta / 200.0), stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=True, supercruise=False, reason="COMPASS_SWITCH_DELTA:" + str(switch_delta) + ":PRESENTATION_VOTES:S" + str(solid_votes) + ":H" + str(hollow_votes))
            if solid_votes >= 2 or hollow_votes >= 2:
                break
        probe_heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
        probe_status = observe_status_flags()
        if probe_status["massLock"] or probe_status["overHeating"] or probe_status["fsdHyperdriveCharging"] or (probe_heat["state"] == "KNOWN" and probe_heat["percent"] >= PREALIGN_HEAT_CANCEL_PERCENT):
            cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "PREALIGN_HEAT_OR_STATUS_GATE", probe_status["sourceTimestamp"])
            fail("Supercruise prealignment burst crossed its local heat or Status safety gate")
        if not ownership_confirmed:
            cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "PREALIGN_OWNERSHIP_NOT_CONFIRMED", probe_status["sourceTimestamp"])
            fail("Supercruise prealignment could not confirm Compass ownership from prompt or pre/post-charge differential")
        if solid_votes >= 2:
            final_target = solid_target
        elif hollow_votes >= 2:
            final_target = hollow_target
        else:
            sample = cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "PREALIGN_PRESENTATION_NOT_STABLE", probe_status["sourceTimestamp"])
            status = observe_status_flags()
            heat_result = wait_for_safe_heat(target_name, sample, turn_count, final_observation, selected_control, stable_heading_confirmations)
            sample = heat_result["sample"]
            continue
        emit_update("PROBING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=None, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=True, supercruise=False, reason="SHORT_CHARGE_PROBE")
        if final_target["presentation"] == "SOLID":
            visible_attempt = action.try_call(id="elite-dangerous/escape-vector-visible-position", inputs={})
            if visible_attempt["ok"] and visible_attempt["output"]["target"]["state"] == "DETECTED":
                emit_update("ALIGNING_VISIBLE_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=None, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=True, supercruise=False, reason="VISIBLE_ESCAPE_VECTOR_ROI_DETECTED")
                visible_result = action.call(id="elite-dangerous/align-visible-target", inputs={"targetName": "ESCAPE VECTOR", "stopBeforeAlign": False, "positionSource": "ESCAPE_VECTOR"})
                status = observe_status_flags()
                if not status["fsdCharging"] or status["fsdHyperdriveCharging"] or status["overHeating"]:
                    fail("visible Escape Vector alignment lost its safe Supercruise charge context")
                emit_update("ALIGNING_VISIBLE_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=None, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=True, supercruise=False, reason="VISIBLE_ESCAPE_VECTOR_ALIGNMENT_COMPLETED")
                visible_alignment_completed = True
                visible_alignment_confirmations = visible_result["stableConfirmations"]
                prealigned = True
                break
            emit_update("PROBING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=None, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=True, supercruise=False, reason="VISIBLE_ESCAPE_VECTOR_ROI_UNKNOWN")
        sample = cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "PREALIGN_PROBE_COMPLETE", probe_status["sourceTimestamp"], preserve_snapshot=True)
        status = observe_status_flags()
        if final_target["detected"] and final_target["presentation"] == "SOLID" and final_target["centerDistancePixels"] <= ESCAPE_VECTOR_CENTER_RADIUS_PIXELS:
            prealigned = True
            break
        if not final_target["detected"] or final_target["presentation"] == "UNKNOWN":
            continue
        if final_target["presentation"] == "HOLLOW":
            if prealign_hollow_control == None:
                # A rear projection provides no reliable signed screen-space
                # correction. Pitch is the ship's faster primary turning axis,
                # so walk one fixed great-circle direction until a fresh probe
                # changes the topology to SOLID.
                prealign_hollow_control = "PITCH_UP"
            selected_control = prealign_hollow_control
        else:
            prealign_hollow_control = None
            selected_control = choose_escape_vector_control(final_target)
            if prealign_previous_presentation == "SOLID" and prealign_previous_distance != None and final_target["centerDistancePixels"] > prealign_previous_distance + ALIGNMENT_WORSENING_PIXELS and selected_control == prealign_last_control:
                selected_control = opposite(selected_control)
        if selected_control == None:
            fail("Supercruise prealignment probe returned no usable Escape Vector direction")
        prealign_hold_ms = choose_prealignment_hold(final_target)
        emit_update("PREALIGNING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=selected_control, command_hold_ms=prealign_hold_ms, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=False, supercruise=False, reason="TURN_SEGMENT_FROM_CACHED_ONE_SHOT_SNAPSHOT", escape_vector_evidence_state="CACHED_ONE_SHOT")
        run_prealignment_segment(selected_control, prealign_hold_ms)
        prealign_previous_presentation = final_target["presentation"]
        prealign_previous_distance = final_target["centerDistancePixels"]
        prealign_last_control = selected_control
        final_observation = observe_obstruction()
        # The charge-owned Compass snapshot authorizes exactly one pulse.  It
        # is no longer current after that attitude command because cancelling
        # charge removes the Escape Vector marker.  The next pulse requires a
        # fresh short-charge observation.
        final_target = empty_target()
        emit_update("OBSERVING_POST_PULSE_VIEW", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=selected_control, command_hold_ms=prealign_hold_ms, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", fsd_charging=False, supercruise=False, reason="ESCAPE_VECTOR_SNAPSHOT_CONSUMED", escape_vector_evidence_state="EXPIRED")
        heat_result = wait_for_safe_heat(target_name, sample, turn_count, final_observation, selected_control, stable_heading_confirmations)
        sample = heat_result["sample"]
        status = observe_status_flags()
    if not prealigned:
        fail("Escape Vector did not become a centered front marker within bounded short-charge probes")

    precharge_timestamp = status["sourceTimestamp"]
    if not status["fsdCharging"]:
        action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
        reset_failure_compensation(cancel_charge=True)
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    entry_started_ms = task.elapsed_milliseconds()
    # Prealignment snapshots expire when their probe charge is cancelled.
    # Formal entry must obtain fresh charge-owned Compass evidence.
    final_target = empty_target()
    escape_vector_seen = visible_alignment_completed
    missing_count = 0
    unknown_count = 0
    alignment_confirmations = visible_alignment_confirmations
    alignment_commands = 0
    active_lease_id = None
    active_lease_control = None
    previous_presentation = None
    previous_target_distance = None
    last_commanded_control = None
    alignment_cycle = 0
    charge_heat_unknown_count = 0

    for _ in range(ESCAPE_VECTOR_SAMPLE_LIMIT):
        sample += 1
        if active_lease_id != None:
            action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "RENEW", "control": active_lease_control, "leaseId": active_lease_id})
        status = observe_status_flags()
        heat_percent = None
        countdown_overheat_allowed = visible_alignment_completed and status["overHeating"]
        if (status["overHeating"] and not countdown_overheat_allowed) or status["massLock"] or status["fsdHyperdriveCharging"]:
            release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
            active_lease_id = None
            active_lease_control = None
            cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "FAST_STATUS_SAFETY_GATE", status["sourceTimestamp"])
            fail("Supercruise escape charge was cancelled by the fast Status safety gate")
        post_command_status = status["sourceTimestamp"] != precharge_timestamp
        if post_command_status and status["supercruise"]:
            release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
            active_lease_id = None
            active_lease_control = None
            reset_failure_compensation()
            break
        if not post_command_status or not status["fsdCharging"]:
            emit_update("WAITING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=None, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=alignment_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=False, fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=False, elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="WAITING_FOR_SUPERCRUISE_CHARGING_BEFORE_COMPASS_SWITCH")
            task.sleep(milliseconds=50)
            continue

        alignment_cycle += 1
        if alignment_cycle % CHARGE_HEAT_SAMPLE_CYCLES == 0:
            # OCR must see a momentarily stable cockpit. A live leased attitude
            # input displaced the HUD far enough to move the heat digits out of
            # even the expanded ROI. Stop the lease locally, let flight assist
            # damp the view, sample heat, then let Compass choose the next lease.
            if active_lease_id != None:
                release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
                active_lease_id = None
                active_lease_control = None
            task.sleep(milliseconds=HEAT_VIEW_SETTLE_MS)
            heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
            heat_percent = heat["percent"]
            if heat["state"] == "KNOWN":
                charge_heat_unknown_count = 0
            else:
                charge_heat_unknown_count += 1
            heat_limit = POST_ALIGNMENT_COUNTDOWN_HEAT_CANCEL_PERCENT if visible_alignment_completed else CHARGE_HEAT_CANCEL_PERCENT
            heat_phase = "CHECKING_COUNTDOWN_HEAT" if visible_alignment_completed else "CHECKING_CHARGE_HEAT"
            heat_reason = ("COUNTDOWN_HEAT_KNOWN_LIMIT_160" if heat["state"] == "KNOWN" else "COUNTDOWN_HEAT_UNKNOWN_TOLERATED") if visible_alignment_completed else ("HEAT_KNOWN_LIMIT_75" if heat["state"] == "KNOWN" else "HEAT_UNKNOWN_RETRY")
            emit_update(heat_phase, target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=alignment_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason=heat_reason)
            # ship-heat is an OCR Action and may finish after the FSD transition.
            # Re-read the fast Status source before issuing any cancellation so
            # a successful Supercruise entry always wins this race.
            post_heat_status = observe_status_flags()
            if post_heat_status["supercruise"]:
                status = post_heat_status
                release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
                active_lease_id = None
                active_lease_control = None
                reset_failure_compensation()
                break
            unknown_heat_limit_reached = not visible_alignment_completed and charge_heat_unknown_count >= CHARGE_HEAT_UNKNOWN_LIMIT
            if (heat["state"] == "KNOWN" and heat_percent >= heat_limit) or unknown_heat_limit_reached:
                release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
                active_lease_id = None
                active_lease_control = None
                cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "CHARGE_HEAT_GATE", status["sourceTimestamp"])
                fail("Supercruise escape charge was cancelled by the local visual heat gate")

        if visible_alignment_completed:
            emit_update("WAITING_FSD_COUNTDOWN", target_name, sample, turn_count, observation=final_observation, target=empty_target(), selected_control=None, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=alignment_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="VISIBLE_ALIGNMENT_COMPLETE_RETICLE_NO_LONGER_REQUIRED")
            task.sleep(milliseconds=100)
            continue

        attempt = action.try_call(id="elite-dangerous/compass", inputs={})
        if not attempt["ok"]:
            release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
            active_lease_id = None
            active_lease_control = None
            missing_count += 1
            emit_update("WAITING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=None, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=alignment_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="COMPASS_UNAVAILABLE:" + str(attempt["errorCode"]))
            if missing_count >= ESCAPE_VECTOR_MISSING_LIMIT:
                cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "ESCAPE_VECTOR_NOT_VISIBLE", status["sourceTimestamp"])
                fail("Escape Vector compass did not become visible within eight bounded observations")
            task.sleep(milliseconds=100)
            continue
        final_target = attempt["output"]["target"]
        if not final_target["detected"]:
            release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
            active_lease_id = None
            active_lease_control = None
            missing_count += 1
            if missing_count >= ESCAPE_VECTOR_MISSING_LIMIT:
                cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "ESCAPE_VECTOR_NOT_DETECTED", status["sourceTimestamp"])
                fail("Escape Vector compass target was not detected within eight bounded observations")
            task.sleep(milliseconds=100)
            continue
        escape_vector_seen = True
        missing_count = 0
        if final_target["presentation"] == "UNKNOWN":
            release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
            active_lease_id = None
            active_lease_control = None
            unknown_count += 1
            emit_update("ALIGNING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=0, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="AMBIGUOUS_COMPASS_PRESENTATION")
            if unknown_count >= ESCAPE_VECTOR_UNKNOWN_LIMIT:
                cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "ESCAPE_VECTOR_PRESENTATION_UNKNOWN", status["sourceTimestamp"])
                fail("Escape Vector compass presentation remained UNKNOWN")
            task.sleep(milliseconds=100)
            continue
        unknown_count = 0
        if previous_presentation == "SOLID" and final_target["presentation"] == "HOLLOW" and last_commanded_control != None:
            release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
            active_lease_id = None
            active_lease_control = None
            selected_control = opposite(last_commanded_control)
            transition_hold_ms = FINE_ALIGNMENT_HOLD_MS
            action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": selected_control, "holdMs": transition_hold_ms})
            alignment_commands += 1
            emit_update("ALIGNING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=selected_control, command_hold_ms=transition_hold_ms, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="SOLID_TO_HOLLOW_PRESENTATION_CORRECTION")
            previous_presentation = final_target["presentation"]
            previous_target_distance = final_target["centerDistancePixels"]
            last_commanded_control = selected_control
            task.sleep(milliseconds=50)
            continue
        centered = final_target["presentation"] == "SOLID" and final_target["centerDistancePixels"] <= ESCAPE_VECTOR_CENTER_RADIUS_PIXELS
        desired_sustained_control = choose_sustained_escape_control(final_target)
        sustained_distance_worsened = final_target["presentation"] == "SOLID" and previous_presentation == "SOLID" and previous_target_distance != None and final_target["centerDistancePixels"] > previous_target_distance + ALIGNMENT_WORSENING_PIXELS
        if desired_sustained_control != None and sustained_distance_worsened and desired_sustained_control == last_commanded_control:
            desired_sustained_control = opposite(desired_sustained_control)
        if active_lease_id != None and desired_sustained_control != active_lease_control:
            released_control = active_lease_control
            release_escape_hold(active_lease_control, active_lease_id, cancel_charge=True)
            active_lease_id = None
            active_lease_control = None
            selected_control = opposite(released_control)
            action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": selected_control, "holdMs": TRANSITION_BRAKE_MS})
            alignment_commands += 1
            emit_update("ALIGNING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=selected_control, command_hold_ms=TRANSITION_BRAKE_MS, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="SUSTAINED_RELEASE_BRAKE")
            previous_presentation = final_target["presentation"]
            previous_target_distance = final_target["centerDistancePixels"]
            last_commanded_control = selected_control
            task.sleep(milliseconds=50)
            continue
        if centered:
            alignment_confirmations += 1
            selected_control = None
            emit_update("VERIFYING_ESCAPE_VECTOR_ALIGNMENT", target_name, sample, turn_count, observation=final_observation, target=final_target, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=alignment_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="ESCAPE_VECTOR_CENTERED")
        else:
            alignment_confirmations = 0
            selected_control = choose_escape_vector_control(final_target)
            if desired_sustained_control != None:
                if active_lease_id == None:
                    hold_result = action.call(id="elite-dangerous/ship-attitude-hold", inputs={"operation": "START", "control": desired_sustained_control})
                    active_lease_id = hold_result["leaseId"]
                    active_lease_control = desired_sustained_control
                    reset_failure_compensation(active_lease_control, active_lease_id, cancel_charge=True)
                    alignment_commands += 1
                selected_control = active_lease_control
                last_commanded_control = selected_control
                emit_update("ALIGNING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="DISTANCE_TREND_REVERSAL" if sustained_distance_worsened and selected_control != choose_sustained_escape_control(final_target) else "ESCAPE_VECTOR_SUSTAINED_CONTROL")
            else:
                hold_ms = choose_alignment_hold(final_target)
                distance_worsened = previous_presentation == "SOLID" and previous_target_distance != None and final_target["centerDistancePixels"] > previous_target_distance + ALIGNMENT_WORSENING_PIXELS
                if distance_worsened and selected_control == last_commanded_control:
                    selected_control = opposite(selected_control)
                    hold_ms = FINE_ALIGNMENT_HOLD_MS
                action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": selected_control, "holdMs": hold_ms})
                alignment_commands += 1
                last_commanded_control = selected_control
                emit_update("ALIGNING_ESCAPE_VECTOR", target_name, sample, turn_count, observation=final_observation, target=final_target, selected_control=selected_control, command_hold_ms=hold_ms, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], supercruise=status["supercruise"], elapsed_ms=task.elapsed_milliseconds() - entry_started_ms, reason="DISTANCE_TREND_REVERSAL" if distance_worsened and selected_control != choose_escape_vector_control(final_target) else "ESCAPE_VECTOR_PULSE_CONTROL")
        previous_presentation = final_target["presentation"]
        previous_target_distance = final_target["centerDistancePixels"]
        task.sleep(milliseconds=50)

    status = observe_status_flags()
    if not status["supercruise"]:
        if status["fsdCharging"]:
            cancel_supercruise_charge(target_name, sample, turn_count, final_observation, final_target, selected_control, "SUPERCRUISE_ENTRY_TIMEOUT", status["sourceTimestamp"])
        fail("Supercruise did not enter after bounded Escape Vector alignment")
    if not escape_vector_seen or alignment_confirmations < ESCAPE_VECTOR_CENTER_CONFIRMATIONS_REQUIRED:
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
        fail("Supercruise entered without a durable centered Escape Vector observation")

    elapsed = 0
    while elapsed < SUPERCRUISE_ESCAPE_DURATION_MS:
        task.sleep(milliseconds=1000)
        elapsed += 1000
        sample += 1
        status = observe_status_flags()
        heat_percent = None
        if not status["supercruise"] or status["overHeating"]:
            fail("Supercruise stellar escape lost cruise state or crossed the heat gate")
        if elapsed % 5000 == 0:
            final_observation = observe_obstruction()
            emit_update("SUPERCRUISE_ESCAPE", target_name, sample, turn_count, observation=final_observation, target=final_target, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=alignment_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, supercruise=True, elapsed_ms=elapsed, reason="ESCAPE_VECTOR_STELLAR_CLEARANCE")
            if final_observation["state"] == "BLOCKING":
                fail("Supercruise escape turned back toward a blocking stellar view")

    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    final_observation = observe_obstruction()
    status = observe_status_flags()
    if final_observation["state"] != "CLEAR" or not status["supercruise"] or status["overHeating"]:
        fail("final Supercruise stellar escape evidence is not safe and CLEAR")
    action.clear_on_failure()
    emit_update("COMPLETED", target_name, sample, turn_count, observation=final_observation, target=final_target, stable_heading_confirmations=stable_heading_confirmations, alignment_confirmations=alignment_confirmations, throttle=0, mass_lock="OFF", heat_percent=heat_percent, supercruise=True, elapsed_ms=elapsed, reason="READY_TO_RESTORE_HYPERSPACE_DESTINATION")
    return {"schemaVersion":6,"task":"CLEAR_HYPERSPACE_OCCLUSION","completed":True,"targetName":target_name,"initialTurnCount":turn_count,"stableInitialHeadingConfirmations":stable_heading_confirmations,"maximumChargeStartHeatPercent":MAX_CHARGE_START_HEAT_PERCENT,"chargeStartHeatPercent":charge_start_heat_percent,"escapeVectorDetected":escape_vector_seen,"escapeVectorAlignmentConfirmations":alignment_confirmations,"escapeVectorAlignmentCommands":alignment_commands,"supercruiseEscapeDurationMs":elapsed,"finalOcclusionState":final_observation["state"],"finalStellarCoverageRatio":final_observation["stellarCoverageRatio"],"finalSupercruiseConfirmed":True,"restoreHyperspaceDestinationRequired":True,"finalCommandedThrottle":0}
