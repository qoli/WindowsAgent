NAV_SETTLE_MS = 150
ACTIVATE_SETTLE_MS = 600
APPLY_SETTLE_MS = 750

def emit_update(phase, profile, command, count, reason):
    stream.emit(
        type="action.set-commodity-market-view.update",
        payload={
            "phase": phase,
            "profile": profile,
            "command": command,
            "count": count,
            "reason": reason,
        },
    )

def send_many(profile, phase, control, count, reason, settle_ms=NAV_SETTLE_MS):
    emit_update(phase, profile, control + "_X_" + str(count), count, reason)
    for _ in range(count):
        action.call(id="elite-dangerous/ui-control", inputs={"control": control})
        task.sleep(milliseconds=settle_ms)
    return count

def activate(profile, phase, reason, settle_ms=ACTIVATE_SETTLE_MS):
    emit_update(phase, profile, "SELECT", 1, reason)
    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
    task.sleep(milliseconds=settle_ms)
    return 1

def buy_all_goods(profile):
    count = 0
    count += send_many(profile, "RESETTING_FILTERS", "DOWN", 3, "MARKET_ENTRY_FOCUS_TO_FILTERS")
    count += activate(profile, "RESETTING_FILTERS", "OPEN_FILTERS")
    count += send_many(profile, "RESETTING_FILTERS", "DOWN", 20, "CLAMP_FILTER_FOCUS_TO_BOTTOM")
    count += send_many(profile, "RESETTING_FILTERS", "RIGHT", 5, "CLAMP_FILTER_FOCUS_TO_RESET")
    count += activate(profile, "RESETTING_FILTERS", "ACTIVATE_RESET_FILTERS")
    count += send_many(profile, "APPLYING_FILTERS", "LEFT", 5, "CLAMP_FILTER_FOCUS_TO_LEFT")
    count += send_many(profile, "APPLYING_FILTERS", "RIGHT", 1, "FOCUS_APPLY")
    count += activate(profile, "APPLYING_FILTERS", "ACTIVATE_APPLY", settle_ms=APPLY_SETTLE_MS)
    count += send_many(profile, "SELECTING_MODE", "UP", 3, "FOCUS_BUY_FROM_FILTERS")
    count += activate(profile, "SELECTING_MODE", "ACTIVATE_BUY")
    count += send_many(profile, "FOCUSING_LIST", "RIGHT", 1, "FOCUS_FIRST_GOODS_ROW")
    return count

def sell_single_cargo(profile):
    count = 0
    count += send_many(profile, "RESETTING_FILTERS", "DOWN", 3, "MARKET_ENTRY_FOCUS_TO_FILTERS")
    count += activate(profile, "RESETTING_FILTERS", "OPEN_FILTERS")
    count += send_many(profile, "RESETTING_FILTERS", "DOWN", 20, "CLAMP_FILTER_FOCUS_TO_BOTTOM")
    count += send_many(profile, "RESETTING_FILTERS", "RIGHT", 2, "FOCUS_RESET_FILTERS")
    count += activate(profile, "RESETTING_FILTERS", "ACTIVATE_RESET_FILTERS")
    count += send_many(profile, "SELECTING_FILTER", "UP", 10, "FOCUS_CARGO_FILTER_ROW")
    count += send_many(profile, "SELECTING_FILTER", "RIGHT", 1, "FOCUS_CARGO_FILTER_VALUE")
    count += activate(profile, "SELECTING_FILTER", "ACTIVATE_CARGO_FILTER")
    count += send_many(profile, "APPLYING_FILTERS", "DOWN", 15, "CLAMP_FILTER_FOCUS_TO_BOTTOM")
    count += send_many(profile, "APPLYING_FILTERS", "LEFT", 3, "CLAMP_FILTER_FOCUS_TO_CANCEL")
    count += send_many(profile, "APPLYING_FILTERS", "RIGHT", 1, "FOCUS_APPLY")
    count += activate(profile, "APPLYING_FILTERS", "ACTIVATE_APPLY", settle_ms=APPLY_SETTLE_MS)
    count += send_many(profile, "SELECTING_MODE", "UP", 2, "FOCUS_SELL_FROM_FILTERS")
    count += activate(profile, "SELECTING_MODE", "ACTIVATE_SELL")
    count += send_many(profile, "FOCUSING_LIST", "RIGHT", 1, "FOCUS_ONLY_SELLABLE_CARGO_ROW")
    return count

def main(ctx):
    profile = ctx.inputs["profile"]
    control_count = buy_all_goods(profile) if profile == "BUY_ALL_GOODS" else sell_single_cargo(profile)
    expected = 42 if profile == "BUY_ALL_GOODS" else 63
    if control_count != expected:
        fail("Commodity Market view replay produced an impossible control count")
    emit_update("COMPLETED", profile, None, control_count, "FIXED_MARKET_VIEW_REPLAY_COMPLETED")
    return {
        "schemaVersion": 1,
        "task": "SET_COMMODITY_MARKET_VIEW",
        "completed": True,
        "profile": profile,
        "filterReplayCompleted": True,
        "listFocusCommanded": True,
        "controlCount": control_count,
    }
