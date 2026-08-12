MIN_DETECTION_CONFIDENCE = 0.70
MIN_RECOGNITION_CONFIDENCE = 0.75
MIN_SPEED_DETECTION_CONFIDENCE = 0.55
LABELS = ["DISTANCE", "SPEED", "ALIGNMENT"]

def normalize(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ":
            result += character
    return result

def normalize_speed(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789./":
            result += character
    return result

def has_speed_unit_suffix(letters, prefix):
    for index in range(len(letters) - len(prefix) + 1):
        if letters[index:index + len(prefix)] != prefix:
            continue
        suffix = letters[index + len(prefix):]
        if len(suffix) >= 1 and len(suffix) <= 2 and suffix[len(suffix) - 1] == "S":
            return True
    return False

def supercruise_speed_unit(raw):
    for region in raw["regions"]:
        if region["detectionConfidence"] < MIN_SPEED_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
            continue
        text = normalize_speed(region["text"])
        unit_letters = normalize(region["text"])
        if has_speed_unit_suffix(unit_letters, "KM"):
            return "KM/S"
        if has_speed_unit_suffix(unit_letters, "MM"):
            return "MM/S"
        if unit_letters == "C" and len(text) >= 2:
            return "C"
    return None

def main(ctx):
    raw = action.call(id="elite-dangerous/request-docking-distance-regions", inputs={})
    matched = []
    raw_texts = []
    for region in raw["regions"]:
        raw_texts.append(region["text"])
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
            continue
        text = normalize(region["text"])
        for label in LABELS:
            if text == label and label not in matched:
                matched.append(label)
    speed_raw = None
    speed_unit = None
    speed_raw_texts = []
    if len(matched) < 2:
        speed_raw = action.call(id="elite-dangerous/ship-speed-text-regions", inputs={})
        for region in speed_raw["regions"]:
            speed_raw_texts.append(region["text"])
        speed_unit = supercruise_speed_unit(speed_raw)
    state = "ACTIVE" if len(matched) >= 2 or speed_unit != None else "INACTIVE"
    return {
        "schemaVersion": 1,
        "supercruiseHud": {
            "state": state,
            "matchedLabels": matched,
            "requiredLabelCount": 2,
            "evidence": {
                "rawTexts": raw_texts,
                "minimumDetectionConfidence": MIN_DETECTION_CONFIDENCE,
                "minimumRecognitionConfidence": MIN_RECOGNITION_CONFIDENCE,
                "minimumSpeedDetectionConfidence": MIN_SPEED_DETECTION_CONFIDENCE,
                "speedRawTexts": speed_raw_texts,
                "supercruiseSpeedUnit": speed_unit,
            },
        },
        "timing": raw["timing"] if speed_raw == None else {"dashboard": raw["timing"], "speed": speed_raw["timing"]},
    }
