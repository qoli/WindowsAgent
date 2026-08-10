TURN_LIMIT = 14
TURN_HOLD_MS = 600
PROBE_MIN_DELTA = 0.003
STABLE_HEADING_CONFIRMATIONS_REQUIRED = 3
HEADING_SETTLE_MS = 1500
MAX_CHARGE_START_HEAT_PERCENT = 60
COOLING_SAMPLE_LIMIT = 30
HEAT_CONFIRMATIONS_REQUIRED = 3
NORMAL_ESCAPE_LIMIT = 30
NORMAL_STELLAR_ESCAPE_DURATION_MS = 120000
NORMAL_STELLAR_ESCAPE_HEAT_LIMIT = 75
CHARGE_HEAT_CANCEL_PERCENT = 90
SUPERCRUISE_ENTRY_TIMEOUT_MS = 15000
SUPERCRUISE_CANCEL_LIMIT = 12
SUPERCRUISE_ESCAPE_DURATION_MS = 30000
CURRENT_STATUS_WAIT_LIMIT = 40
STATUS_OBSERVER_RETRY_LIMIT = 3

def emit_update(phase, target_name, sample, turn_count, observation=None, selected_control=None, probe_delta=None, clear_confirmations=0, stable_heading_confirmations=0, throttle=0, mass_lock=None, heat_percent=None, fsd_charging=None, fsd_hyperdrive_charging=None, fsd_cooldown=None, over_heating=None, supercruise=None, elapsed_ms=0, reason=None):
    stream.emit(type="action.clear-hyperspace-occlusion.update", payload={
        "phase": phase,
        "targetName": target_name,
        "sample": sample,
        "turnCount": turn_count,
        "occlusionState": None if observation == None else observation["state"],
        "stellarCoverageRatio": None if observation == None else observation["stellarCoverageRatio"],
        "centerCoverageRatio": None if observation == None else observation["centerCoverageRatio"],
        "directionConfidence": None if observation == None else observation["directionConfidence"],
        "recommendedControl": None if observation == None else observation["recommendedControl"],
        "selectedControl": selected_control,
        "probeDelta": probe_delta,
        "clearConfirmations": clear_confirmations,
        "stableHeadingConfirmations": stable_heading_confirmations,
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

def settle_clear_heading(target_name, sample, turn_count, selected_control):
    task.sleep(milliseconds=1000)
    task.sleep(milliseconds=HEADING_SETTLE_MS - 1000)
    confirmations = 0
    observation = None
    for _ in range(STABLE_HEADING_CONFIRMATIONS_REQUIRED):
        sample += 1
        observation = observe_obstruction()
        if observation["state"] == "CLEAR":
            confirmations += 1
        else:
            confirmations = 0
        emit_update("STABILIZING_HEADING", target_name, sample, turn_count, observation=observation, selected_control=selected_control, clear_confirmations=confirmations, stable_heading_confirmations=confirmations, reason="NO_ATTITUDE_INPUT_SETTLE_GATE")
        if confirmations < STABLE_HEADING_CONFIRMATIONS_REQUIRED:
            task.sleep(milliseconds=500)
    return {"sample": sample, "observation": observation, "confirmations": confirmations}

def has_flag(value, bit_value):
    return (value // bit_value) % 2 == 1

def observe_status_flags():
    status = None
    last_error = None
    for attempt_index in range(STATUS_OBSERVER_RETRY_LIMIT):
        attempt = action.try_call(id="elite-dangerous/filesystem/status", inputs={})
        if attempt["ok"]:
            status = attempt["output"]
            break
        last_error = attempt["error"]
        if attempt_index + 1 < STATUS_OBSERVER_RETRY_LIMIT:
            task.sleep(milliseconds=250)
    if status == None:
        fail("Status.json observer failed after three bounded attempts: " + last_error)
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

def opposite(control):
    if control == "PITCH_UP":
        return "PITCH_DOWN"
    if control == "PITCH_DOWN":
        return "PITCH_UP"
    if control == "YAW_LEFT":
        return "YAW_RIGHT"
    return "YAW_LEFT"

def cancel_supercruise_charge(target_name, sample, turn_count, observation, selected_control, reason):
    action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    for _ in range(SUPERCRUISE_CANCEL_LIMIT):
        task.sleep(milliseconds=250)
        sample += 1
        status = observe_status_flags()
        emit_update("CANCELLING_CHARGE", target_name, sample, turn_count, observation=observation, selected_control=selected_control, throttle=0, mass_lock="ON" if status["massLock"] else "OFF", fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], reason=reason)
        if not status["fsdCharging"]:
            return sample
    fail("FSD charging did not cancel within the bounded Status flag window")

def main(ctx):
    target_name = ctx.inputs["targetName"]
    normal_space_separation_confirmed = ctx.inputs.get("normalSpaceSeparationConfirmed", False)
    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    sample = 1
    turn_count = 0
    initial = observe_obstruction()
    emit_update("VERIFYING_OBSTRUCTION", target_name, sample, turn_count, observation=initial, reason="CURRENT_FORWARD_VIEW")
    selected_control = initial["recommendedControl"]
    probing = selected_control == None
    if probing:
        selected_control = "PITCH_UP"
    previous_ratio = initial["stellarCoverageRatio"]
    final_observation = initial
    stable_heading_confirmations = 0
    if initial["state"] == "CLEAR":
        settled = settle_clear_heading(target_name, sample, turn_count, selected_control)
        sample = settled["sample"]
        final_observation = settled["observation"]
        stable_heading_confirmations = settled["confirmations"]
    for _ in range(TURN_LIMIT):
        if stable_heading_confirmations >= STABLE_HEADING_CONFIRMATIONS_REQUIRED:
            break
        action.call(id="elite-dangerous/ship-attitude-control", inputs={"control": selected_control, "holdMs": TURN_HOLD_MS})
        turn_count += 1
        sample += 1
        observation = observe_obstruction()
        delta = previous_ratio - observation["stellarCoverageRatio"]
        phase = "PROBING_DIRECTION" if probing else "TURNING_AWAY"
        reason = "COVERAGE_DECREASING" if delta >= PROBE_MIN_DELTA else "COVERAGE_NOT_DECREASING"
        emit_update(phase, target_name, sample, turn_count, observation=observation, selected_control=selected_control, probe_delta=delta, reason=reason)
        final_observation = observation
        if observation["state"] == "CLEAR":
            settled = settle_clear_heading(target_name, sample, turn_count, selected_control)
            sample = settled["sample"]
            final_observation = settled["observation"]
            stable_heading_confirmations = settled["confirmations"]
            if stable_heading_confirmations >= STABLE_HEADING_CONFIRMATIONS_REQUIRED:
                break
            # A CLEAR frame captured during a turn may coast back into PARTIAL.
            # Resume bounded correction; charging is still forbidden.
            previous_ratio = final_observation["stellarCoverageRatio"]
            if final_observation["recommendedControl"] != None and final_observation["directionConfidence"] >= 0.16:
                selected_control = final_observation["recommendedControl"]
            continue
        if probing:
            if delta >= PROBE_MIN_DELTA:
                probing = False
            elif delta <= -PROBE_MIN_DELTA:
                selected_control = opposite(selected_control)
                probing = False
        elif delta <= -PROBE_MIN_DELTA:
            selected_control = opposite(selected_control)
            probing = True
        elif abs(delta) < PROBE_MIN_DELTA and observation["recommendedControl"] != None and observation["directionConfidence"] >= 0.16:
            selected_control = observation["recommendedControl"]
        previous_ratio = observation["stellarCoverageRatio"]
    if stable_heading_confirmations < STABLE_HEADING_CONFIRMATIONS_REQUIRED:
        fail("stellar obstruction did not become stably CLEAR within the bounded trend-guided turns")

    ship = action.call(id="elite-dangerous/ship-status", inputs={})["shipStatus"]
    landing_gear = ship["landingGear"]["state"]
    cargo_scoop = ship["cargoScoop"]["state"]
    if landing_gear != "OFF" or cargo_scoop != "OFF":
        fail("stellar escape requires visually confirmed Landing Gear and Cargo Scoop OFF")

    status = None
    for _ in range(CURRENT_STATUS_WAIT_LIMIT):
        sample += 1
        status = observe_status_flags()
        if status["freshness"] == "CURRENT":
            break
        emit_update("WAITING_CURRENT_STATUS", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="ON" if status["massLock"] else "OFF", fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], reason="AWAITING_CURRENT_STATUS_SNAPSHOT")
        task.sleep(milliseconds=1000)
    if status["freshness"] != "CURRENT":
        fail("Status.json did not become current within the bounded preflight wait")
    if status["overHeating"] or status["fsdCharging"] or status["fsdCooldown"]:
        fail("FSD preflight Status flags are not idle and cool")

    if status["massLock"]:
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
        mass_lock_off_confirmations = 0
        for _ in range(NORMAL_ESCAPE_LIMIT):
            task.sleep(milliseconds=1000)
            sample += 1
            status = observe_status_flags()
            if not status["massLock"]:
                mass_lock_off_confirmations += 1
            else:
                mass_lock_off_confirmations = 0
            emit_update("NORMAL_ESCAPE", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="ON" if status["massLock"] else "OFF", fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], reason="WAITING_FOR_MASS_LOCK_OFF")
            if status["overHeating"] or status["fsdCharging"]:
                fail("unsafe Status flag appeared during normal-space escape")
            if mass_lock_off_confirmations >= 2:
                break
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
        if mass_lock_off_confirmations < 2:
            fail("Mass Lock did not become stably OFF during bounded normal-space escape")

    # A clear forward ROI proves direction, not safe distance from the star's
    # thermal zone. Build normal-space separation before asking the FSD to
    # sustain a charge.
    if normal_space_separation_confirmed:
        emit_update("RESUMING_AFTER_STELLAR_ESCAPE", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", elapsed_ms=NORMAL_STELLAR_ESCAPE_DURATION_MS, reason="SUPERVISOR_CONFIRMED_PRIOR_DURABLE_ESCAPE_CHECKPOINT")

    normal_space_escape_performed = not normal_space_separation_confirmed
    if normal_space_escape_performed:
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
    normal_escape_elapsed = 0
    normal_escape_heat = None
    movement_confirmations = 0
    while normal_space_escape_performed and normal_escape_elapsed < NORMAL_STELLAR_ESCAPE_DURATION_MS:
        task.sleep(milliseconds=1000)
        normal_escape_elapsed += 1000
        sample += 1
        status = observe_status_flags()
        if normal_escape_elapsed % 5000 == 0:
            normal_escape_heat_result = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
            normal_escape_heat = normal_escape_heat_result["percent"]
            if normal_escape_heat_result["state"] == "KNOWN" and normal_escape_heat >= NORMAL_STELLAR_ESCAPE_HEAT_LIMIT:
                action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                fail("ship heat reached the normal-space stellar escape limit before FSD charging")
        if normal_escape_elapsed >= 5000 and normal_escape_elapsed <= 8000:
            escape_speed_result = action.call(id="elite-dangerous/ship-speed", inputs={})
            escape_speed = escape_speed_result["speed"]
            # Exact OCR may become UNKNOWN when cockpit inertia moves the
            # number within the reviewed ROI. The independent glyph observer
            # still provides current visual evidence that the value is not
            # the stopped slashed zero.
            nonzero_visual = escape_speed["state"] in ["MOVING", "LOW_SPEED"] or escape_speed_result["evidence"]["zeroGlyph"]["state"] == "NOT_ZERO"
            if nonzero_visual:
                movement_confirmations += 1
            if normal_escape_elapsed == 8000 and movement_confirmations < 2:
                action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
                fail("100% normal-space stellar escape did not produce two current non-zero visual speed confirmations in four samples")
        emit_update("NORMAL_STELLAR_ESCAPE", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="ON" if status["massLock"] else "OFF", heat_percent=normal_escape_heat, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=normal_escape_elapsed, reason="BUILDING_PRECHARGE_STELLAR_SEPARATION")
        if status["overHeating"] or status["fsdCharging"] or status["fsdHyperdriveCharging"] or status["supercruise"]:
            fail("unsafe Status flag appeared during normal-space stellar escape")
    if normal_space_escape_performed:
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})

    heat_confirmations = 0
    heat_percent = None
    for _ in range(COOLING_SAMPLE_LIMIT):
        sample += 1
        heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
        heat_percent = heat["percent"]
        if heat["state"] == "KNOWN" and heat_percent <= MAX_CHARGE_START_HEAT_PERCENT:
            heat_confirmations += 1
        else:
            heat_confirmations = 0
        status = observe_status_flags()
        emit_update("COOLING_BEFORE_CHARGE", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="ON" if status["massLock"] else "OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], reason="MAX_START_HEAT:" + str(MAX_CHARGE_START_HEAT_PERCENT))
        if status["overHeating"] or status["fsdCharging"] or status["fsdCooldown"] or status["massLock"]:
            fail("FSD safety Status changed while waiting for charge-start heat")
        if heat_confirmations >= HEAT_CONFIRMATIONS_REQUIRED:
            break
        task.sleep(milliseconds=500)
    if heat_confirmations < HEAT_CONFIRMATIONS_REQUIRED:
        fail("ship heat did not produce three known observations at or below the charge-start limit")

    entry_performed = not status["supercruise"]
    if entry_performed:
        precharge_status_timestamp = status["sourceTimestamp"]
        action.call(id="elite-dangerous/supercruise-control", inputs={"command": "TOGGLE"})
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})
        entry_started_ms = task.elapsed_milliseconds()
        next_charge_heat_sample_ms = 1000
        charge_heat_percent = heat_percent
        entered = False
        while task.elapsed_milliseconds() - entry_started_ms < SUPERCRUISE_ENTRY_TIMEOUT_MS:
            task.sleep(milliseconds=250)
            sample += 1
            status = observe_status_flags()
            elapsed = task.elapsed_milliseconds() - entry_started_ms
            if elapsed >= next_charge_heat_sample_ms:
                charge_heat = action.call(id="elite-dangerous/ship-heat", inputs={})["heat"]
                next_charge_heat_sample_ms += 1000
                if charge_heat["state"] == "KNOWN":
                    charge_heat_percent = charge_heat["percent"]
            new_status_evidence = status["sourceTimestamp"] != precharge_status_timestamp
            emit_update("ENTERING_SUPERCRUISE", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="ON" if status["massLock"] else "OFF", heat_percent=charge_heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=elapsed, reason="STATUS_FLAG_ENTRY_GATE" if new_status_evidence else "AWAITING_POST_COMMAND_STATUS_TIMESTAMP")
            if charge_heat_percent >= CHARGE_HEAT_CANCEL_PERCENT:
                cancel_supercruise_charge(target_name, sample, turn_count, final_observation, selected_control, "CHARGE_HEAT_LIMIT")
                fail("Supercruise charge was cancelled at the visual heat safety limit")
            if not new_status_evidence:
                continue
            if status["supercruise"]:
                entered = True
                break
            if status["overHeating"] or status["massLock"] or status["fsdHyperdriveCharging"]:
                cancel_supercruise_charge(target_name, sample, turn_count, final_observation, selected_control, "UNSAFE_ENTRY_STATUS")
                fail("Supercruise charge was cancelled after an unsafe Status flag")
        if not entered:
            status = observe_status_flags()
            if status["sourceTimestamp"] != precharge_status_timestamp and status["fsdCharging"]:
                cancel_supercruise_charge(target_name, sample, turn_count, final_observation, selected_control, "ENTRY_TIMEOUT")
            fail("Supercruise did not enter within fifteen seconds after pre-charge alignment")
    else:
        action.call(id="elite-dangerous/set-throttle", inputs={"percent": 100})

    elapsed = 0
    while elapsed < SUPERCRUISE_ESCAPE_DURATION_MS:
        task.sleep(milliseconds=1000)
        elapsed += 1000
        sample += 1
        status = observe_status_flags()
        if not status["supercruise"] or status["overHeating"]:
            fail("Supercruise stellar escape lost cruise state or overheated")
        if elapsed % 5000 == 0:
            final_observation = observe_obstruction()
            emit_update("SUPERCRUISE_ESCAPE", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=100, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=elapsed, reason="TANGENTIAL_STELLAR_CLEARANCE")
            if final_observation["state"] == "BLOCKING":
                fail("Supercruise escape turned back into a blocking stellar view")

    action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    final_observation = observe_obstruction()
    status = observe_status_flags()
    emit_update("STOPPING", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=elapsed, reason="SET_THROTTLE_0")
    if final_observation["state"] != "CLEAR" or not status["supercruise"] or status["overHeating"]:
        fail("final Supercruise stellar escape evidence is not safe and CLEAR")
    action.clear_on_failure()
    emit_update("COMPLETED", target_name, sample, turn_count, observation=final_observation, selected_control=selected_control, clear_confirmations=stable_heading_confirmations, stable_heading_confirmations=stable_heading_confirmations, throttle=0, mass_lock="OFF", heat_percent=heat_percent, fsd_charging=status["fsdCharging"], fsd_hyperdrive_charging=status["fsdHyperdriveCharging"], fsd_cooldown=status["fsdCooldown"], over_heating=status["overHeating"], supercruise=status["supercruise"], elapsed_ms=elapsed, reason="STELLAR_ESCAPE_COMPLETED")
    return {"schemaVersion":4,"task":"CLEAR_HYPERSPACE_OCCLUSION","completed":True,"targetName":target_name,"turnCount":turn_count,"stableHeadingConfirmations":stable_heading_confirmations,"selectedControl":selected_control,"maximumChargeStartHeatPercent":MAX_CHARGE_START_HEAT_PERCENT,"chargeStartHeatPercent":heat_percent,"normalSpaceEscapePerformed":normal_space_escape_performed,"supercruiseEntryPerformed":entry_performed,"supercruiseEscapeDurationMs":elapsed,"finalOcclusionState":final_observation["state"],"finalStellarCoverageRatio":final_observation["stellarCoverageRatio"],"finalSupercruiseConfirmed":True,"finalCommandedThrottle":0}
