NAVIGATION_SETTLE_MS = 200
SERVICE_SETTLE_MS = 750
FOCUS_CONFIRM_SETTLE_MS = 150

def send(control, settle_ms=NAVIGATION_SETTLE_MS):
    result = action.call(id="elite-dangerous/ui-control", inputs={"control": control})
    task.sleep(milliseconds=settle_ms)
    return result

def repeat(control, count):
    for _ in range(count):
        send(control)

def observe_service_focus():
    return action.call(id="elite-dangerous/station-service-focus", inputs={})["focus"]

def require_known_focus(focus, context):
    if focus["state"] == "UNKNOWN" or focus["index"] == None:
        fail(context + ": station service focus is UNKNOWN: " + focus["reason"])

def main(ctx):
    activate_auto_launch = ctx.inputs["activateAutoLaunch"]
    # Elite Dangerous clamps repeated DOWN navigation at DISEMBARK. Starting
    # from any item in the docked cockpit menu, four presses therefore provide
    # a deterministic focus baseline without interpreting HUD colors.
    repeat("DOWN", 4)

    # DISEMBARK -> AUTO LAUNCH -> STARPORT SERVICES -> remembered service tile.
    repeat("UP", 3)
    first_focus = observe_service_focus()
    require_known_focus(first_focus, "first observation")
    task.sleep(milliseconds=FOCUS_CONFIRM_SETTLE_MS)
    second_focus = observe_service_focus()
    require_known_focus(second_focus, "second observation")
    if first_focus["state"] != second_focus["state"] or first_focus["index"] != second_focus["index"]:
        fail("station service focus changed between confirmation observations")

    right_moves_to_refuel = (4 - second_focus["index"]) % 4
    repeat("RIGHT", right_moves_to_refuel)
    refuel_focus = second_focus
    if right_moves_to_refuel > 0:
        refuel_focus = observe_service_focus()
    if refuel_focus["state"] != "REFUEL" or refuel_focus["index"] != 0:
        fail("calculated service navigation did not visually confirm REFUEL")
    refuel = send("SELECT", SERVICE_SETTLE_MS)

    # Service tiles remain keyboard-focusable when no purchase is needed.
    # SELECT is consequently a safe idempotent attempt for both services.
    send("RIGHT")
    repair_focus = observe_service_focus()
    if repair_focus["state"] != "REPAIR" or repair_focus["index"] != 1:
        fail("RIGHT from REFUEL did not visually confirm REPAIR")
    repair = send("SELECT", SERVICE_SETTLE_MS)

    # Re-establish the same lower boundary and focus AUTO LAUNCH. Safe mode
    # intentionally stops before the final SELECT for visual confirmation.
    repeat("DOWN", 4)
    send("UP")
    auto_launch = None
    if activate_auto_launch:
        auto_launch = send("SELECT")

    return {
        "schemaVersion": 1,
        "task": "PREPARE_AUTO_LAUNCH",
        "completed": True,
        "controlCount": 15 + right_moves_to_refuel + (1 if activate_auto_launch else 0),
        "focusBaseline": "DISEMBARK",
        "initialServiceFocus": second_focus["state"],
        "rightMovesToRefuel": right_moves_to_refuel,
        "refuelFocusConfirmed": True,
        "repairFocusConfirmed": True,
        "refuelAttempted": True,
        "repairAttempted": True,
        "autoLaunchSelected": activate_auto_launch,
        "awaitingFinalSelect": not activate_auto_launch,
        "refuelCommand": refuel,
        "repairCommand": repair,
        "autoLaunchCommand": auto_launch,
    }
