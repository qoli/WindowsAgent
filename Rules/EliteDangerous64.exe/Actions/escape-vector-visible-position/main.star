MIN_DETECTION_CONFIDENCE = 0.70
MIN_RECOGNITION_CONFIDENCE = 0.75
SCREEN_CENTER_X = 960.0
SCREEN_CENTER_Y = 540.0
MARKER_FROM_TEXT_RIGHT_X = -5.0
MARKER_FROM_TEXT_BOTTOM_Y = 40.0
MAX_PAIR_CENTER_X_DELTA = 24.0
MAX_PAIR_VERTICAL_GAP = 16.0

def normalize(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ":
            result += character
    return result

def bounds(points):
    left = points[0]["x"]
    right = points[0]["x"]
    top = points[0]["y"]
    bottom = points[0]["y"]
    for point in points:
        left = min(left, point["x"])
        right = max(right, point["x"])
        top = min(top, point["y"])
        bottom = max(bottom, point["y"])
    return {"left": left, "right": right, "top": top, "bottom": bottom, "centerX": (left + right) / 2.0}

def square_root(value):
    if value <= 0:
        return 0.0
    guess = value
    for _ in range(12):
        guess = (guess + value / guess) / 2.0
    return guess

def unknown(raw_texts, timing, reason):
    return {
        "schemaVersion": 1,
        "target": {"state": "UNKNOWN", "referenceX": None, "referenceY": None, "offsetX": None, "offsetY": None, "centerDistancePixels": None, "reason": reason, "rawTexts": raw_texts},
        "timing": timing,
    }

def main(ctx):
    raw = action.call(id="elite-dangerous/supercruise-target-text-regions-lower", inputs={})
    escape_regions = []
    vector_regions = []
    raw_texts = []
    for region in raw["regions"]:
        raw_texts.append(region["text"])
        if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
            continue
        text = normalize(region["text"])
        if text == "ESCAPE":
            escape_regions.append(region)
        elif text == "VECTOR":
            vector_regions.append(region)
    if len(escape_regions) != 1 or len(vector_regions) != 1:
        return unknown(raw_texts, raw["timing"], "ESCAPE_VECTOR_TEXT_NOT_UNIQUE")
    escape_box = bounds(escape_regions[0]["referencePoints"])
    vector_box = bounds(vector_regions[0]["referencePoints"])
    vertical_gap = vector_box["top"] - escape_box["bottom"]
    if abs(escape_box["centerX"] - vector_box["centerX"]) > MAX_PAIR_CENTER_X_DELTA or vertical_gap < -8.0 or vertical_gap > MAX_PAIR_VERTICAL_GAP:
        return unknown(raw_texts, raw["timing"], "ESCAPE_VECTOR_TEXT_PAIR_GEOMETRY_INVALID")
    text_right = max(escape_box["right"], vector_box["right"])
    text_bottom = max(escape_box["bottom"], vector_box["bottom"])
    reference_x = text_right + MARKER_FROM_TEXT_RIGHT_X
    reference_y = text_bottom + MARKER_FROM_TEXT_BOTTOM_Y
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
            "reason": "ESCAPE_VECTOR_TWO_LINE_LABEL_TO_RETICLE_OFFSET_APPLIED",
            "rawTexts": raw_texts,
        },
        "timing": raw["timing"],
    }
