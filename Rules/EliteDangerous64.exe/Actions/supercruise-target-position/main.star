MIN_DETECTION_CONFIDENCE = 0.70
MIN_RECOGNITION_CONFIDENCE = 0.75
SCREEN_CENTER_X = 960.0
SCREEN_CENTER_Y = 540.0
LABEL_TO_MARKER_X = 30.0
LABEL_TO_MARKER_Y = 8.0

def normalize(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            result += character
    return result

def bounds(points):
    left = points[0]["x"]
    top = points[0]["y"]
    bottom = points[0]["y"]
    for point in points:
        if point["x"] < left:
            left = point["x"]
        if point["y"] < top:
            top = point["y"]
        if point["y"] > bottom:
            bottom = point["y"]
    return {"left": left, "centerY": (top + bottom) / 2.0}

def square_root(value):
    if value <= 0:
        return 0.0
    guess = value
    for _ in range(12):
        guess = (guess + value / guess) / 2.0
    return guess

def main(ctx):
    target_name = ctx.inputs["targetName"]
    expected = normalize(target_name)
    raw = action.call(id="elite-dangerous/supercruise-target-text-regions", inputs={})
    matches = []
    raw_texts = []
    for region in raw["regions"]:
        raw_texts.append(region["text"])
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
            continue
        if normalize(region["text"]) == expected:
            matches.append(region)
    if len(matches) != 1:
        return {
            "schemaVersion": 1,
            "target": {"state": "UNKNOWN", "referenceX": None, "referenceY": None, "offsetX": None, "offsetY": None, "centerDistancePixels": None, "reason": "TARGET_TEXT_NOT_UNIQUE", "rawTexts": raw_texts},
            "timing": raw["timing"],
        }
    region = matches[0]
    box = bounds(region["referencePoints"])
    reference_x = box["left"] - LABEL_TO_MARKER_X
    reference_y = box["centerY"] - LABEL_TO_MARKER_Y
    offset_x = reference_x - SCREEN_CENTER_X
    offset_y = reference_y - SCREEN_CENTER_Y
    return {
        "schemaVersion": 1,
        "target": {
            "state": "DETECTED",
            "referenceX": reference_x,
            "referenceY": reference_y,
            "offsetX": offset_x,
            "offsetY": offset_y,
            "centerDistancePixels": square_root(offset_x * offset_x + offset_y * offset_y),
            "reason": "TARGET_LABEL_TO_MARKER_OFFSET_APPLIED",
            "rawTexts": raw_texts,
        },
        "timing": raw["timing"],
    }
