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

def one_edit_or_exact(actual, expected):
    if actual == expected:
        return True
    if len(actual) < 5 or len(expected) < 5:
        return False
    difference = len(actual) - len(expected)
    if difference < -1 or difference > 1:
        return False
    if difference == 0:
        mismatches = 0
        for index in range(len(actual)):
            if actual[index] != expected[index]:
                mismatches += 1
                if mismatches > 1:
                    return False
        return True
    shorter = actual if len(actual) < len(expected) else expected
    longer = expected if len(actual) < len(expected) else actual
    short_index = 0
    long_index = 0
    skipped = False
    while short_index < len(shorter) and long_index < len(longer):
        if shorter[short_index] == longer[long_index]:
            short_index += 1
            long_index += 1
        elif skipped:
            return False
        else:
            skipped = True
            long_index += 1
    return True

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
    bands = [
        action.call(id="elite-dangerous/supercruise-target-text-regions", inputs={}),
        action.call(id="elite-dangerous/supercruise-target-text-regions-lower", inputs={}),
    ]
    matches = []
    raw_texts = []
    for raw in bands:
        for region in raw["regions"]:
            raw_texts.append(region["text"])
            if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
                continue
            if not one_edit_or_exact(normalize(region["text"]), expected):
                continue
            candidate_box = bounds(region["referencePoints"])
            duplicate_index = None
            for index in range(len(matches)):
                existing_box = bounds(matches[index]["referencePoints"])
                if abs(candidate_box["left"] - existing_box["left"]) <= 8.0 and abs(candidate_box["centerY"] - existing_box["centerY"]) <= 8.0:
                    duplicate_index = index
                    break
            if duplicate_index == None:
                matches.append(region)
            elif region["recognitionConfidence"] > matches[duplicate_index]["recognitionConfidence"]:
                matches[duplicate_index] = region
    if len(matches) != 1:
        return {
            "schemaVersion": 1,
            "target": {"state": "UNKNOWN", "referenceX": None, "referenceY": None, "offsetX": None, "offsetY": None, "centerDistancePixels": None, "reason": "TARGET_TEXT_NOT_UNIQUE", "rawTexts": raw_texts},
            "timing": {"bands": [bands[0]["timing"], bands[1]["timing"]]},
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
        "timing": {"bands": [bands[0]["timing"], bands[1]["timing"]]},
    }
