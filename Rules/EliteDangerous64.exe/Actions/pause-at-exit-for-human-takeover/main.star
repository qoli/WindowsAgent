POLL_MS = 350
OPEN_ATTEMPTS = 3
OBSERVATION_ATTEMPTS = 4
DOWN_TO_BOTTOM_COUNT = 8
MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.60
EXIT_FOCUS_FILL_MINIMUM = 0.55

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
    context = region["leftContext"]
    pixels = context["pixels"]
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

def observe():
    raw = action.call(id="elite-dangerous/pause-menu-text-regions", inputs={})
    resume_seen = False
    exit_region = None
    raw_texts = []
    for region in raw["regions"]:
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
            continue
        text = normalize(region["text"])
        raw_texts.append(region["text"])
        if text == "RESUME":
            resume_seen = True
        elif text == "EXIT":
            exit_region = region
    ratio = focus_fill(exit_region) if exit_region != None else 0.0
    if resume_seen and exit_region != None and ratio >= EXIT_FOCUS_FILL_MINIMUM:
        state = "EXIT_FOCUSED"
        reason = "PAUSE_MENU_EXIT_FOCUS_CONFIRMED"
    elif resume_seen and exit_region != None:
        state = "PAUSE_MENU"
        reason = "PAUSE_MENU_VISIBLE_EXIT_NOT_FOCUSED"
    elif not resume_seen and exit_region == None:
        state = "ABSENT"
        reason = "PAUSE_MENU_LABELS_ABSENT"
    else:
        state = "UNKNOWN"
        reason = "PAUSE_MENU_LABELS_INCOMPLETE"
    return {"state": state, "reason": reason, "exitFocusFillRatio": ratio, "rawTexts": raw_texts}

def emit(phase, attempt, observation, last_command=None):
    stream.emit(type="action.pause-at-exit-for-human-takeover.update", payload={
        "phase": phase,
        "attempt": attempt,
        "menuState": observation["state"],
        "exitFocusFillRatio": observation["exitFocusFillRatio"],
        "lastCommand": last_command,
        "reason": observation["reason"],
    })

def observe_stable_exit(phase, attempt):
    confirmations = 0
    last = None
    for _ in range(OBSERVATION_ATTEMPTS):
        last = observe()
        if last["state"] == "EXIT_FOCUSED":
            confirmations += 1
            if confirmations >= 2:
                emit(phase, attempt, last)
                return last
        else:
            confirmations = 0
        emit(phase, attempt, last)
        task.sleep(milliseconds=POLL_MS)
    return last

def main(ctx):
    initial = observe()
    emit("OBSERVING", 0, initial)
    if initial["state"] == "EXIT_FOCUSED":
        confirmed = observe_stable_exit("VERIFYING_EXIT_FOCUS", 0)
        if confirmed["state"] == "EXIT_FOCUSED":
            return {"schemaVersion": 1, "task": "PAUSE_AT_EXIT_FOR_HUMAN_TAKEOVER", "completed": True, "pauseMenuConfirmed": True, "exitFocused": True, "selectSent": False, "openAttempts": 0, "finalObservation": confirmed}

    opened = initial
    open_attempts = 0
    for attempt in range(1, OPEN_ATTEMPTS + 1):
        if opened["state"] not in ["PAUSE_MENU", "EXIT_FOCUSED"]:
            action.call(id="elite-dangerous/ui-control", inputs={"control": "BACK"})
            open_attempts = attempt
            emit("OPENING_PAUSE_MENU", attempt, opened, last_command="BACK")
            task.sleep(milliseconds=750)
            opened = observe()
            emit("OPENING_PAUSE_MENU", attempt, opened)
        if opened["state"] in ["PAUSE_MENU", "EXIT_FOCUSED"]:
            break
    if opened["state"] not in ["PAUSE_MENU", "EXIT_FOCUSED"]:
        fail("PAUSE_MENU_NOT_CONFIRMED: BACK did not produce the reviewed pause menu")

    if opened["state"] != "EXIT_FOCUSED":
        for _ in range(DOWN_TO_BOTTOM_COUNT):
            action.call(id="elite-dangerous/ui-control", inputs={"control": "DOWN"})
            task.sleep(milliseconds=120)
        emit("FOCUSING_EXIT", open_attempts, opened, last_command="DOWN_X8")

    confirmed = observe_stable_exit("VERIFYING_EXIT_FOCUS", open_attempts)
    if confirmed["state"] != "EXIT_FOCUSED":
        fail("PAUSE_MENU_EXIT_NOT_FOCUSED: bounded DOWN navigation did not visually confirm EXIT focus")
    stream.activity(message="Flight paused at EXIT for human takeover", level="warning")
    return {"schemaVersion": 1, "task": "PAUSE_AT_EXIT_FOR_HUMAN_TAKEOVER", "completed": True, "pauseMenuConfirmed": True, "exitFocused": True, "selectSent": False, "openAttempts": open_attempts, "finalObservation": confirmed}
