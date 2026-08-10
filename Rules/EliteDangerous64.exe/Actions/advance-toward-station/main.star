POLL_MS = 250
BASELINE_MAX_SAMPLES = 5
MAX_DISTANCE_STEP_METERS = 1000.0
TREND_EPSILON_METERS = 10.0
MOVING_AWAY_LIMIT = 2

def emit_update(phase, sample, commanded_throttle, observed_distance, accepted_distance, target_distance, elapsed_ms, remaining_ms, trend, last_command=None, reason=None):
    stream.emit(
        type="action.advance-toward-station.update",
        payload={
            "phase": phase,
            "sample": sample,
            "commandedThrottle": commanded_throttle,
            "observedStationDistanceMeters": observed_distance,
            "acceptedStationDistanceMeters": accepted_distance,
            "stopAtStationDistanceMeters": target_distance,
            "elapsedMs": elapsed_ms,
            "remainingMs": remaining_ms,
            "stationDistanceTrend": trend,
            "evidenceSource": "TARGET_LOCK_HUD_OCR",
            "lastCommand": last_command,
            "reason": reason,
        },
    )

def observe_station_distance():
    attempt = action.try_call(id="elite-dangerous/request-docking-range", inputs={})
    if not attempt["ok"]:
        return {"ok": False, "reason": "STATION_DISTANCE_OBSERVATION_FAILED: " + attempt["error"], "distance": None}
    gate = attempt["output"]["requestDockingRange"]
    if gate["state"] == "UNKNOWN" or gate["distanceMeters"] == None:
        return {"ok": False, "reason": "STATION_DISTANCE_UNKNOWN: " + gate["evidence"]["reason"], "distance": None}
    return {"ok": True, "reason": gate["evidence"]["reason"], "distance": gate["distanceMeters"]}

def remaining_time(max_duration_ms, elapsed_ms):
    remaining = max_duration_ms - elapsed_ms
    return 0 if remaining < 0 else remaining

def stop_and_fail(message, sample, observed_distance, accepted_distance, target_distance, elapsed_ms, max_duration_ms, trend, reason):
    stop_result = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
    action.clear_on_failure()
    emit_update("STOPPING", sample, 0, observed_distance, accepted_distance, target_distance, elapsed_ms, remaining_time(max_duration_ms, elapsed_ms), trend, last_command=stop_result["control"], reason=reason)
    fail(message)

def completed_output(final_phase, throttle_percent, target_distance, initial_distance, final_distance, elapsed_ms, sample_count, stop_result):
    return {
        "schemaVersion": 1,
        "task": "ADVANCE_TOWARD_STATION",
        "completed": True,
        "finalPhase": final_phase,
        "throttlePercent": throttle_percent,
        "stopAtStationDistanceMeters": target_distance,
        "initialStationDistanceMeters": initial_distance,
        "finalStationDistanceMeters": final_distance,
        "elapsedMs": elapsed_ms,
        "sampleCount": sample_count,
        "stopResult": stop_result,
    }

def main(ctx):
    throttle_percent = int(ctx.inputs["throttlePercent"])
    target_distance = float(ctx.inputs["stopAtStationDistanceMeters"])
    max_duration_ms = int(ctx.inputs["maxDurationMs"])

    stream.activity(message="Establishing Station HUD distance baseline", level="info")
    baseline_candidate = None
    initial_distance = None
    sample = 0
    for _ in range(BASELINE_MAX_SAMPLES):
        sample += 1
        observation = observe_station_distance()
        observed_distance = observation["distance"]
        trend = "UNKNOWN" if not observation["ok"] else "SEEKING_BASELINE"
        if observation["ok"]:
            if baseline_candidate == None:
                baseline_candidate = observed_distance
            elif abs(observed_distance - baseline_candidate) <= MAX_DISTANCE_STEP_METERS:
                initial_distance = observed_distance
                emit_update("PREFLIGHT", sample, None, observed_distance, initial_distance, target_distance, 0, max_duration_ms, "BASELINE", reason=observation["reason"])
                break
            else:
                baseline_candidate = observed_distance
                trend = "OUTLIER"
        emit_update("PREFLIGHT", sample, None, observed_distance, None, target_distance, 0, max_duration_ms, trend, reason=observation["reason"])
        if sample < BASELINE_MAX_SAMPLES:
            task.sleep(milliseconds=POLL_MS)

    if initial_distance == None:
        fail("Station target-lock HUD distance did not produce a stable baseline; no throttle was applied")

    if initial_distance <= target_distance:
        stop_result = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
        emit_update("COMPLETED", sample, 0, initial_distance, initial_distance, target_distance, 0, max_duration_ms, "STOPPED", last_command=stop_result["control"], reason="TARGET_ALREADY_REACHED")
        stream.activity(message="Station target distance already satisfies the requested stop point", level="info")
        return completed_output("TARGET_ALREADY_REACHED", throttle_percent, target_distance, initial_distance, initial_distance, 0, sample, stop_result)

    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    throttle_result = action.call(id="elite-dangerous/set-throttle", inputs={"percent": throttle_percent})
    started_ms = task.elapsed_milliseconds()
    accepted_distance = initial_distance
    moving_away_count = 0
    emit_update("ADVANCING", sample, throttle_percent, initial_distance, accepted_distance, target_distance, 0, max_duration_ms, "BASELINE", last_command=throttle_result["control"], reason="FORWARD_THROTTLE_APPLIED")
    stream.activity(message="Advancing toward Station HUD distance " + str(target_distance) + " m", level="info")

    while True:
        elapsed_ms = task.elapsed_milliseconds() - started_ms
        if elapsed_ms >= max_duration_ms:
            stop_and_fail("Timed advance reached maxDurationMs before the Station HUD distance target", sample, None, accepted_distance, target_distance, elapsed_ms, max_duration_ms, "TIMED_OUT", "MAX_DURATION_REACHED")

        observation = observe_station_distance()
        sample += 1
        elapsed_ms = task.elapsed_milliseconds() - started_ms
        observed_distance = observation["distance"]
        if not observation["ok"]:
            stop_and_fail("Station target-lock HUD distance became unavailable while advancing", sample, observed_distance, accepted_distance, target_distance, elapsed_ms, max_duration_ms, "UNKNOWN", observation["reason"])

        if observed_distance <= target_distance:
            stop_result = action.call(id="elite-dangerous/set-throttle", inputs={"percent": 0})
            action.clear_on_failure()
            emit_update("STOPPING", sample, 0, observed_distance, accepted_distance, target_distance, elapsed_ms, remaining_time(max_duration_ms, elapsed_ms), "TARGET_CANDIDATE", last_command=stop_result["control"], reason="TARGET_DISTANCE_CANDIDATE_STOPPED")
            stream.activity(message="Station distance candidate reached; throttle stopped before confirmation", level="info")
            task.sleep(milliseconds=POLL_MS)
            confirmation = observe_station_distance()
            sample += 1
            confirmed_distance = confirmation["distance"]
            elapsed_ms = task.elapsed_milliseconds() - started_ms
            if not confirmation["ok"] or confirmed_distance > target_distance or abs(confirmed_distance - observed_distance) > MAX_DISTANCE_STEP_METERS:
                reason = confirmation["reason"] if not confirmation["ok"] else "TARGET_DISTANCE_CONFIRMATION_INCONSISTENT"
                emit_update("STOPPING", sample, 0, confirmed_distance, observed_distance, target_distance, elapsed_ms, remaining_time(max_duration_ms, elapsed_ms), "UNKNOWN", last_command=stop_result["control"], reason=reason)
                fail("Station HUD distance target could not be confirmed after throttle stopped")
            emit_update("COMPLETED", sample, 0, confirmed_distance, confirmed_distance, target_distance, elapsed_ms, remaining_time(max_duration_ms, elapsed_ms), "CONFIRMED", last_command=stop_result["control"], reason="STATION_DISTANCE_REACHED")
            stream.activity(message="Station HUD distance reached and throttle stopped", level="info")
            return completed_output("STATION_DISTANCE_REACHED", throttle_percent, target_distance, initial_distance, confirmed_distance, elapsed_ms, sample, stop_result)

        delta = observed_distance - accepted_distance
        if abs(delta) > MAX_DISTANCE_STEP_METERS:
            stop_and_fail("Station HUD distance changed discontinuously while advancing", sample, observed_distance, accepted_distance, target_distance, elapsed_ms, max_duration_ms, "OUTLIER", "STATION_DISTANCE_DISCONTINUITY")
        trend = "STABLE"
        if delta < -TREND_EPSILON_METERS:
            trend = "DECREASING"
            moving_away_count = 0
        elif delta > TREND_EPSILON_METERS:
            trend = "INCREASING"
            moving_away_count += 1
        else:
            moving_away_count = 0
        accepted_distance = observed_distance
        emit_update("ADVANCING", sample, throttle_percent, observed_distance, accepted_distance, target_distance, elapsed_ms, remaining_time(max_duration_ms, elapsed_ms), trend, reason=observation["reason"])
        if moving_away_count >= MOVING_AWAY_LIMIT:
            stop_and_fail("Station HUD distance increased for two consecutive trusted samples", sample, observed_distance, accepted_distance, target_distance, elapsed_ms, max_duration_ms, "INCREASING", "STATION_DISTANCE_MOVING_AWAY")
        task.sleep(milliseconds=POLL_MS)
