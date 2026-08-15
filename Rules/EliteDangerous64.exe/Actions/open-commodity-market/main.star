UI_SETTLE_MS = 250
SERVICE_TRANSITION_MS = 1000
MARKET_TRANSITION_MS = 1000
OCR_SETTLE_MS = 250
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
COMMODITY_MARKET_X = 395
COMMODITY_MARKET_Y = 704

def emit_update(phase, operation, station_name, command=None, observation=None, reason=None):
    stream.emit(
        type="action.open-commodity-market.update",
        payload={
            "phase": phase,
            "operation": operation,
            "stationName": station_name,
            "command": command,
            "observation": observation,
            "reason": reason,
        },
    )

def normalize_text(text):
    normalized = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            normalized += character
    return normalized

def trusted(region):
    return region["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and region["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE

def trusted_text(raw):
    values = []
    for region in raw["regions"]:
        if trusted(region):
            values.append(normalize_text(region["text"]))
    return values

def inspect_docked_menu(raw):
    values = trusted_text(raw)
    return {
        "starportServices": "STARPORTSERVICES" in values,
        "autoLaunch": "AUTOLAUNCH" in values,
        "disembark": "DISEMBARK" in values,
    }

def inspect_market(raw, station_name):
    values = trusted_text(raw)
    expected_station = normalize_text(station_name)
    mode = None
    if "BUYFROMMARKET" in values:
        mode = "BUY"
    if "SELLTOMARKET" in values:
        if mode != None:
            mode = "AMBIGUOUS"
        else:
            mode = "SELL"
    return {
        "marketTitle": "COMMODITIESMARKET" in values,
        "stationConfirmed": expected_station in values,
        "mode": mode,
    }

def observe_docked_menu_stable(operation, station_name):
    previous = False
    count = 0
    last = None
    for _ in range(6):
        last = inspect_docked_menu(action.call(id="elite-dangerous/docked-cockpit-menu-text-regions", inputs={}))
        count += 1
        known = last["starportServices"] and last["autoLaunch"] and last["disembark"]
        emit_update("PREFLIGHT", operation, station_name, observation=last, reason="EXACT_DOCKED_MENU_LABELS_REQUIRED")
        if known and previous:
            return count
        previous = known
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("current screen did not show the complete docked cockpit menu")

def observe_market_stable(operation, station_name, phase, required_mode=None):
    previous = False
    count = 0
    last = None
    for _ in range(8):
        last = inspect_market(action.call(id="elite-dangerous/commodity-market-header-text-regions", inputs={}), station_name)
        count += 1
        known = last["marketTitle"] and last["stationConfirmed"] and last["mode"] != None and last["mode"] != "AMBIGUOUS"
        if required_mode != None:
            known = known and last["mode"] == required_mode
        emit_update(phase, operation, station_name, observation=last, reason="EXACT_MARKET_STATION_AND_MODE_REQUIRED")
        if known and previous:
            return {"count": count, "observation": last}
        previous = known
        task.sleep(milliseconds=OCR_SETTLE_MS)
    fail("Commodity Market header did not confirm the exact Station and mode")

def send(control, operation, station_name):
    emit_update("NAVIGATING", operation, station_name, command=control, reason="DOCKED_MENU_CLAMPED_NAVIGATION")
    action.call(id="elite-dangerous/ui-control", inputs={"control": control})
    task.sleep(milliseconds=UI_SETTLE_MS)

def activate_pointer_target(x, y, operation, station_name, reason):
    # In the Starport Services and Commodity Market surfaces the injected
    # pointer establishes focus, but a click is not accepted as activation by
    # Elite Dangerous. Confirm the now-focused Rule-owned target through the
    # game's binding-resolved UI_Select control.
    emit_update("NAVIGATING", operation, station_name, command="POINTER_CLICK", observation={"x": x, "y": y}, reason=reason + "_FOCUS")
    action.call(id="elite-dangerous/pointer-click", inputs={"x": x, "y": y, "holdMs": 40})
    task.sleep(milliseconds=UI_SETTLE_MS)
    emit_update("NAVIGATING", operation, station_name, command="SELECT", observation={"x": x, "y": y}, reason=reason + "_ACTIVATE")
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})

def main(ctx):
    operation = ctx.inputs["operation"]
    station_name = ctx.inputs["stationName"]
    observation_count = observe_docked_menu_stable(operation, station_name)

    # From this point onward the Action may have entered Starport Services or
    # the market even when a later OCR postcondition fails. The shared cleanup
    # safely restores the cockpit from either surface.
    action.on_failure(id="elite-dangerous/exit-commodity-market", inputs={"dialogMayBeOpen": True})

    for _ in range(4):
        send("DOWN", operation, station_name)
    for _ in range(2):
        send("UP", operation, station_name)
    send("SELECT", operation, station_name)
    task.sleep(milliseconds=SERVICE_TRANSITION_MS)

    emit_update("OPENING_MARKET", operation, station_name, observation={"x": COMMODITY_MARKET_X, "y": COMMODITY_MARKET_Y}, reason="RULE_OWNED_COMMODITY_MARKET_TILE")
    activate_pointer_target(COMMODITY_MARKET_X, COMMODITY_MARKET_Y, operation, station_name, "RULE_OWNED_COMMODITY_MARKET_TILE")
    task.sleep(milliseconds=MARKET_TRANSITION_MS)
    initial = observe_market_stable(operation, station_name, "CONFIRMING_MARKET")
    observation_count += initial["count"]
    initial_mode = initial["observation"]["mode"]

    profile = "BUY_ALL_GOODS" if operation == "BUY" else "SELL_SINGLE_CARGO"
    expected_controls = 42 if operation == "BUY" else 63
    emit_update("NORMALIZING_VIEW", operation, station_name, observation={"profile": profile, "initialMode": initial_mode}, reason="FIXED_COMMODITY_MARKET_VIEW_REQUIRED")
    view = action.call(id="elite-dangerous/set-commodity-market-view", inputs={"profile": profile})
    if not view["completed"] or not view["filterReplayCompleted"] or not view["listFocusCommanded"] or view["profile"] != profile or view["controlCount"] != expected_controls:
        fail("Commodity Market view child returned an invalid mechanical replay result")

    confirmed = observe_market_stable(operation, station_name, "CONFIRMING_MODE", required_mode=operation)
    observation_count += confirmed["count"]

    action.clear_on_failure()
    emit_update("COMPLETED", operation, station_name, observation={"initialMode": initial_mode, "finalMode": operation, "profile": profile, "controlCount": expected_controls}, reason="MARKET_VIEW_AND_MODE_CONFIRMED")
    stream.activity(message="Commodity Market prepared in " + operation + " mode", level="info")
    return {
        "schemaVersion": 1,
        "task": "OPEN_COMMODITY_MARKET",
        "completed": True,
        "operation": operation,
        "stationName": station_name,
        "dockedMenuConfirmed": True,
        "marketTitleConfirmed": True,
        "stationConfirmed": True,
        "modeConfirmed": True,
        "initialMode": initial_mode,
        "marketViewProfile": profile,
        "filterReplayCompleted": True,
        "listFocusCommanded": True,
        "viewControlCount": expected_controls,
        "observationCount": observation_count,
    }
