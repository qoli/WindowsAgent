POLL_MS = 350
OBSERVATION_ATTEMPTS = 4
MAX_EXIT_NAVIGATION_STEPS = 6
EXIT_DESTINATION_ATTEMPTS = 8
MAIN_MENU_ATTEMPTS = 120
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
EXIT_FOCUS_FILL_MINIMUM = 0.05
EXIT_DESTINATION_FOCUS_FILL_MINIMUM = 0.20

def normalize(text):
    value = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            value += character
    return value

def channels(pixel):
    return pixel // 65536, (pixel // 256) % 256, pixel % 256

def focus_fill(region):
    if region == None:
        return 0.0
    pixels = region["leftContext"]["pixels"]
    if len(pixels) == 0:
        return 0.0
    orange = 0
    inspected = 0
    for index in range(0, len(pixels), 6):
        red, green, blue = channels(pixels[index])
        inspected += 1
        if red >= 180 and green >= 65 and green <= 220 and blue <= 100 and red >= green + 35:
            orange += 1
    return float(orange) / float(inspected) if inspected > 0 else 0.0

def accepted_regions(raw):
    result = []
    for region in raw["regions"]:
        if region["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and region["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE:
            result.append(region)
    return result

def observe_primary_menu():
    raw = action.call(id="elite-dangerous/pause-menu-text-regions", inputs={})
    resume_seen = False
    exit_region = None
    main_anchors = 0
    continue_seen = False
    raw_texts = []
    for region in accepted_regions(raw):
        text = normalize(region["text"])
        raw_texts.append(region["text"])
        if text == "RESUME":
            resume_seen = True
        elif text == "EXIT":
            exit_region = region
        elif text == "CONTINUE":
            continue_seen = True
        elif text in ["SOCIAL", "GAMEEXTRAS", "OPTIONS", "HELPANDINFO"]:
            main_anchors += 1
    ratio = focus_fill(exit_region)
    if continue_seen and main_anchors >= 2 and not resume_seen:
        state = "MAIN_MENU"
        reason = "MAIN_MENU_EXACT_ANCHORS_CONFIRMED"
    elif resume_seen and exit_region != None and ratio >= EXIT_FOCUS_FILL_MINIMUM:
        state = "EXIT_FOCUSED"
        reason = "PAUSE_MENU_EXIT_FOCUS_CONFIRMED"
    elif resume_seen and exit_region != None:
        state = "PAUSE_MENU"
        reason = "PAUSE_MENU_VISIBLE_EXIT_NOT_FOCUSED"
    elif not resume_seen and exit_region == None and not continue_seen and main_anchors == 0:
        state = "ABSENT"
        reason = "PRIMARY_MENU_LABELS_ABSENT"
    else:
        state = "UNKNOWN"
        reason = "PRIMARY_MENU_LABELS_INCOMPLETE"
    return {"state": state, "reason": reason, "focusFillRatio": ratio, "rawTexts": raw_texts}

def observe_exit_destination_menu():
    raw = action.call(id="elite-dangerous/exit-destination-menu-text-regions", inputs={})
    main_region = None
    desktop_region = None
    raw_texts = []
    for region in accepted_regions(raw):
        text = normalize(region["text"])
        raw_texts.append(region["text"])
        if text == "EXITTOMAINMENU":
            main_region = region
        elif text == "QUITTODESKTOP":
            desktop_region = region
    ratio = focus_fill(main_region)
    desktop_ratio = focus_fill(desktop_region)
    if main_region != None and desktop_region != None and ratio >= EXIT_DESTINATION_FOCUS_FILL_MINIMUM and desktop_ratio < EXIT_DESTINATION_FOCUS_FILL_MINIMUM:
        state = "EXIT_TO_MAIN_MENU_FOCUSED"
        reason = "EXIT_DESTINATION_MAIN_MENU_FOCUS_CONFIRMED"
    elif main_region != None and desktop_region != None and ratio >= EXIT_DESTINATION_FOCUS_FILL_MINIMUM and desktop_ratio >= EXIT_DESTINATION_FOCUS_FILL_MINIMUM:
        state = "UNKNOWN"
        reason = "EXIT_DESTINATION_FOCUS_AMBIGUOUS"
    elif main_region != None and desktop_region != None:
        state = "EXIT_DESTINATION_MENU"
        reason = "EXIT_DESTINATION_MENU_VISIBLE_MAIN_NOT_FOCUSED"
    elif main_region == None and desktop_region == None:
        state = "ABSENT"
        reason = "EXIT_DESTINATION_LABELS_ABSENT"
    else:
        state = "UNKNOWN"
        reason = "EXIT_DESTINATION_LABELS_INCOMPLETE"
    return {"state": state, "reason": reason, "focusFillRatio": ratio, "rawTexts": raw_texts}

def emit(phase, attempt, observation, last_command=None):
    stream.emit(type="action.pause-at-exit-for-human-takeover.update", payload={
        "phase": phase,
        "attempt": attempt,
        "menuState": observation["state"],
        "focusFillRatio": observation["focusFillRatio"],
        "lastCommand": last_command,
        "reason": observation["reason"],
    })

def observe_stable_exit(attempt):
    confirmations = 0
    for _ in range(OBSERVATION_ATTEMPTS):
        current = observe_primary_menu()
        if current["state"] == "EXIT_FOCUSED":
            confirmations += 1
            if confirmations >= 2:
                emit("VERIFYING_EXIT_FOCUS", attempt, current)
                return current
        else:
            confirmations = 0
        emit("VERIFYING_EXIT_FOCUS", attempt, current)
        task.sleep(milliseconds=POLL_MS)
    return None

def observe_stable_exit_destination():
    confirmations = 0
    for attempt in range(EXIT_DESTINATION_ATTEMPTS):
        current = observe_exit_destination_menu()
        if current["state"] == "EXIT_TO_MAIN_MENU_FOCUSED":
            confirmations += 1
            if confirmations >= 2:
                emit("VERIFYING_EXIT_DESTINATION", attempt + 1, current)
                return current
        else:
            confirmations = 0
        emit("VERIFYING_EXIT_DESTINATION", attempt + 1, current)
        task.sleep(milliseconds=500)
    return None

def observe_stable_main_menu():
    confirmations = 0
    for attempt in range(MAIN_MENU_ATTEMPTS):
        current = observe_primary_menu()
        if current["state"] == "MAIN_MENU":
            confirmations += 1
            if confirmations >= 2:
                emit("VERIFYING_MAIN_MENU", attempt + 1, current)
                return current, attempt + 1
        else:
            confirmations = 0
        emit("VERIFYING_MAIN_MENU", attempt + 1, current)
        task.sleep(milliseconds=1000)
    return None, MAIN_MENU_ATTEMPTS

def main(ctx):
    initial = observe_primary_menu()
    emit("OBSERVING", 0, initial)
    opened = initial
    open_attempts = 0
    destination = None

    if initial["state"] == "UNKNOWN":
        destination = observe_stable_exit_destination()
        if destination == None:
            fail("PAUSE_MENU_STATE_UNKNOWN: neither the pause menu nor a stable focused EXIT TO MAIN MENU destination was confirmed")
    else:
        if opened["state"] == "ABSENT":
            action.call(id="elite-dangerous/ui-control", inputs={"control": "PAUSE"})
            open_attempts = 1
            emit("OPENING_PAUSE_MENU", 1, opened, last_command="PAUSE")
            task.sleep(milliseconds=750)
            opened = observe_primary_menu()
            emit("OPENING_PAUSE_MENU", 1, opened)
        if opened["state"] not in ["PAUSE_MENU", "EXIT_FOCUSED"]:
            fail("PAUSE_MENU_NOT_CONFIRMED: PAUSE did not produce the reviewed pause menu")

        if opened["state"] != "EXIT_FOCUSED":
            for _ in range(MAX_EXIT_NAVIGATION_STEPS):
                action.call(id="elite-dangerous/ui-control", inputs={"control": "DOWN"})
                task.sleep(milliseconds=120)
                opened = observe_primary_menu()
                emit("FOCUSING_EXIT", open_attempts, opened, last_command="DOWN")
                if opened["state"] == "EXIT_FOCUSED":
                    break
        if opened["state"] != "EXIT_FOCUSED":
            fail("PAUSE_MENU_EXIT_NOT_FOCUSED: one reviewed menu cycle did not reach EXIT focus")

        confirmed_exit = observe_stable_exit(open_attempts)
        if confirmed_exit == None:
            fail("PAUSE_MENU_EXIT_NOT_FOCUSED: bounded DOWN navigation did not visually confirm EXIT focus")

        action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
        emit("SELECTING_EXIT", 1, confirmed_exit, last_command="SELECT")
        task.sleep(milliseconds=750)
        destination = observe_stable_exit_destination()
        if destination == None:
            fail("EXIT_DESTINATION_MAIN_MENU_NOT_CONFIRMED: EXIT did not produce two fresh focused EXIT TO MAIN MENU observations")

    action.call(id="elite-dangerous/ui-control", inputs={"control": "SELECT"})
    emit("SELECTING_EXIT_TO_MAIN_MENU", 1, destination, last_command="SELECT")
    task.sleep(milliseconds=1000)
    main_menu, post_exit_samples = observe_stable_main_menu()
    if main_menu == None:
        fail("MAIN_MENU_NOT_CONFIRMED_AFTER_EXIT: bounded fresh observations did not confirm the non-flight main menu")

    stream.activity(message="Exited flight to the main menu for human takeover", level="warning")
    return {
        "schemaVersion": 2,
        "task": "PAUSE_AT_EXIT_FOR_HUMAN_TAKEOVER",
        "completed": True,
        "pauseMenuConfirmed": True,
        "exitFocused": True,
        "firstExitSelectSent": True,
        "exitDestinationMenuConfirmed": True,
        "exitToMainMenuFocused": True,
        "exitToMainMenuSelectSent": True,
        "mainMenuConfirmed": True,
        "openAttempts": open_attempts,
        "postExitSamples": post_exit_samples,
        "finalObservation": main_menu,
    }
