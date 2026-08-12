UI_SETTLE_MS = 250
SERVICE_TRANSITION_MS = 1000
MARKET_TRANSITION_MS = 1000
OCR_SETTLE_MS = 250
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
COMMODITY_MARKET_X = 395
COMMODITY_MARKET_Y = 704
BUY_TILE_X = 174
BUY_TILE_Y = 252
SELL_TILE_X = 174
SELL_TILE_Y = 401

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

def main(ctx):
    operation = ctx.inputs["operation"]
    station_name = ctx.inputs["stationName"]
    observation_count = observe_docked_menu_stable(operation, station_name)

    for _ in range(4):
        send("DOWN", operation, station_name)
    for _ in range(2):
        send("UP", operation, station_name)
    send("SELECT", operation, station_name)
    task.sleep(milliseconds=SERVICE_TRANSITION_MS)

    emit_update("OPENING_MARKET", operation, station_name, command="POINTER_CLICK", observation={"x": COMMODITY_MARKET_X, "y": COMMODITY_MARKET_Y}, reason="RULE_OWNED_COMMODITY_MARKET_TILE")
    action.call(id="elite-dangerous/pointer-click", inputs={"x": COMMODITY_MARKET_X, "y": COMMODITY_MARKET_Y, "holdMs": 40})
    task.sleep(milliseconds=MARKET_TRANSITION_MS)
    initial = observe_market_stable(operation, station_name, "CONFIRMING_MARKET")
    observation_count += initial["count"]
    initial_mode = initial["observation"]["mode"]
    action.on_failure(id="elite-dangerous/exit-commodity-market", inputs={"dialogMayBeOpen": False})

    if initial_mode != operation:
        tile_x = BUY_TILE_X if operation == "BUY" else SELL_TILE_X
        tile_y = BUY_TILE_Y if operation == "BUY" else SELL_TILE_Y
        emit_update("SWITCHING_MODE", operation, station_name, command="POINTER_CLICK", observation={"x": tile_x, "y": tile_y, "initialMode": initial_mode}, reason="RULE_OWNED_MARKET_MODE_TILE")
        action.call(id="elite-dangerous/pointer-click", inputs={"x": tile_x, "y": tile_y, "holdMs": 40})
        task.sleep(milliseconds=UI_SETTLE_MS)
        confirmed = observe_market_stable(operation, station_name, "CONFIRMING_MODE", required_mode=operation)
        observation_count += confirmed["count"]

    action.clear_on_failure()
    emit_update("COMPLETED", operation, station_name, observation={"initialMode": initial_mode, "finalMode": operation}, reason="MARKET_STATION_AND_MODE_CONFIRMED")
    stream.activity(message="Commodity Market open in " + operation + " mode", level="info")
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
        "observationCount": observation_count,
    }
