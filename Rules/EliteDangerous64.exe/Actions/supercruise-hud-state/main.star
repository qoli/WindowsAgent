MIN_DETECTION_CONFIDENCE = 0.70
MIN_RECOGNITION_CONFIDENCE = 0.75
LABELS = ["DISTANCE", "SPEED", "ALIGNMENT"]

def normalize(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ":
            result += character
    return result

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
    state = "ACTIVE" if len(matched) >= 2 else "INACTIVE"
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
            },
        },
        "timing": raw["timing"],
    }
