MIN_DETECTION_CONFIDENCE = 0.70
MIN_RECOGNITION_CONFIDENCE = 0.75
SCREEN_CENTER_X = 960.0
SCREEN_CENTER_Y = 540.0
LABEL_TO_MARKER_X = 30.0
# The destination ring is below the OCR label centre. The subtraction at the
# call sites therefore uses a negative offset to move the derived point down.
LABEL_TO_MARKER_Y = -12.5
DUPLICATE_BOX_TOLERANCE_PIXELS = 16.0
MIN_NEAREST_CANDIDATE_SEPARATION_PIXELS = 32.0
MIN_OCCLUDED_WORD_PREFIX = 4
MULTILINE_LEFT_TOLERANCE_PIXELS = 16.0
MULTILINE_MIN_CENTER_GAP_PIXELS = 12.0
MULTILINE_MAX_CENTER_GAP_PIXELS = 36.0

def normalize(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            result += character
    return result

def normalized_words(text):
    words = []
    current = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            current += character
        elif character == " " and len(current) > 0:
            words.append(current)
            current = ""
    if len(current) > 0:
        words.append(current)
    return words

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
    right = points[0]["x"]
    top = points[0]["y"]
    bottom = points[0]["y"]
    for point in points:
        if point["x"] < left:
            left = point["x"]
        if point["x"] > right:
            right = point["x"]
        if point["y"] < top:
            top = point["y"]
        if point["y"] > bottom:
            bottom = point["y"]
    return {"left": left, "right": right, "top": top, "bottom": bottom, "centerY": (top + bottom) / 2.0}

def occluded_word_prefix_matches(actual, expected):
    if len(actual) < MIN_OCCLUDED_WORD_PREFIX or len(actual) > len(expected):
        return False
    expected_prefix = expected[:len(actual)]
    mismatches = 0
    for index in range(len(actual)):
        if actual[index] != expected_prefix[index]:
            mismatches += 1
            if mismatches > 1:
                return False
    return True

def multiline_candidate(first, second):
    first_box = bounds(first["referencePoints"])
    second_box = bounds(second["referencePoints"])
    confidence = first["recognitionConfidence"]
    if second["recognitionConfidence"] < confidence:
        confidence = second["recognitionConfidence"]
    right = first_box["right"] if first_box["right"] > second_box["right"] else second_box["right"]
    return {
        "text": first["text"] + " " + second["text"],
        "detectionConfidence": first["detectionConfidence"],
        "recognitionConfidence": confidence,
        "referencePoints": [
            {"x": first_box["left"], "y": first_box["top"]},
            {"x": right, "y": first_box["top"]},
            {"x": right, "y": second_box["bottom"]},
            {"x": first_box["left"], "y": second_box["bottom"]},
        ],
        "matchReason": "OCCLUDED_TWO_LINE_WORD_PREFIXES_CONFIRMED",
    }

def append_deduplicated(matches, region):
    candidate_box = bounds(region["referencePoints"])
    duplicate_index = None
    for index in range(len(matches)):
        existing_box = bounds(matches[index]["referencePoints"])
        if abs(candidate_box["left"] - existing_box["left"]) <= DUPLICATE_BOX_TOLERANCE_PIXELS and abs(candidate_box["centerY"] - existing_box["centerY"]) <= DUPLICATE_BOX_TOLERANCE_PIXELS:
            duplicate_index = index
            break
    if duplicate_index == None:
        matches.append(region)
    elif region["recognitionConfidence"] > matches[duplicate_index]["recognitionConfidence"]:
        matches[duplicate_index] = region

def square_root(value):
    if value <= 0:
        return 0.0
    guess = value
    for _ in range(12):
        guess = (guess + value / guess) / 2.0
    return guess

def marker_distance(region):
    box = bounds(region["referencePoints"])
    reference_x = box["left"] - LABEL_TO_MARKER_X
    reference_y = box["centerY"] - LABEL_TO_MARKER_Y
    offset_x = reference_x - SCREEN_CENTER_X
    offset_y = reference_y - SCREEN_CENTER_Y
    return square_root(offset_x * offset_x + offset_y * offset_y)

def main(ctx):
    target_name = ctx.inputs["targetName"]
    expected = normalize(target_name)
    expected_words = normalized_words(target_name)
    bands = [
        action.call(id="elite-dangerous/supercruise-target-text-regions", inputs={}),
        action.call(id="elite-dangerous/supercruise-target-text-regions-lower", inputs={}),
        action.call(id="elite-dangerous/supercruise-target-text-regions-lower-wide", inputs={}),
    ]
    matches = []
    raw_texts = []
    for raw in bands:
        eligible_regions = []
        for region in raw["regions"]:
            raw_texts.append(region["text"])
            if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
                continue
            eligible_regions.append(region)
            if not one_edit_or_exact(normalize(region["text"]), expected):
                continue
            append_deduplicated(matches, region)
        if len(expected_words) == 2:
            for first in eligible_regions:
                first_text = normalize(first["text"])
                if not occluded_word_prefix_matches(first_text, expected_words[0]):
                    continue
                first_box = bounds(first["referencePoints"])
                for second in eligible_regions:
                    if second == first:
                        continue
                    second_text = normalize(second["text"])
                    if not occluded_word_prefix_matches(second_text, expected_words[1]):
                        continue
                    second_box = bounds(second["referencePoints"])
                    center_gap = second_box["centerY"] - first_box["centerY"]
                    if abs(second_box["left"] - first_box["left"]) > MULTILINE_LEFT_TOLERANCE_PIXELS or center_gap < MULTILINE_MIN_CENTER_GAP_PIXELS or center_gap > MULTILINE_MAX_CENTER_GAP_PIXELS:
                        continue
                    append_deduplicated(matches, multiline_candidate(first, second))
    if len(matches) == 0:
        return {
            "schemaVersion": 1,
            "target": {"state": "UNKNOWN", "referenceX": None, "referenceY": None, "offsetX": None, "offsetY": None, "centerDistancePixels": None, "reason": "TARGET_TEXT_NOT_FOUND", "rawTexts": raw_texts},
            "timing": {"bands": [bands[0]["timing"], bands[1]["timing"], bands[2]["timing"]]},
        }
    region = matches[0]
    reason = region.get("matchReason", "TARGET_LABEL_TO_MARKER_OFFSET_APPLIED")
    if len(matches) > 1:
        closest_distance = marker_distance(matches[0])
        second_distance = None
        for index in range(1, len(matches)):
            distance = marker_distance(matches[index])
            if distance < closest_distance:
                second_distance = closest_distance
                closest_distance = distance
                region = matches[index]
            elif second_distance == None or distance < second_distance:
                second_distance = distance
        if second_distance == None or second_distance - closest_distance < MIN_NEAREST_CANDIDATE_SEPARATION_PIXELS:
            return {
                "schemaVersion": 1,
                "target": {"state": "UNKNOWN", "referenceX": None, "referenceY": None, "offsetX": None, "offsetY": None, "centerDistancePixels": None, "reason": "TARGET_TEXT_CANDIDATES_AMBIGUOUS", "rawTexts": raw_texts},
                "timing": {"bands": [bands[0]["timing"], bands[1]["timing"], bands[2]["timing"]]},
            }
        reason = "NEAREST_FORWARD_TARGET_LABEL_SELECTED"
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
            "reason": reason,
            "rawTexts": raw_texts,
        },
        "timing": {"bands": [bands[0]["timing"], bands[1]["timing"], bands[2]["timing"]]},
    }
