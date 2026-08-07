MIN_DETECTION_CONFIDENCE = 0.65
MIN_RECOGNITION_CONFIDENCE = 0.60

def normalize_digits(text):
    substitutions = {"S": "5", "I": "1", "L": "1", "O": "0", "B": "8"}
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "0123456789":
            result += character
        elif character in substitutions:
            result += substitutions[character]
        else:
            return None
    return result

def distance(left, right):
    return math.hypot(right["x"] - left["x"], right["y"] - left["y"])

def geometry(region):
    points = region["points"]
    if len(points) != 4:
        return None
    center_x = 0.0
    center_y = 0.0
    for point in points:
        center_x += point["x"]
        center_y += point["y"]
    center_x /= 4.0
    center_y /= 4.0
    width = (distance(points[0], points[1]) + distance(points[3], points[2])) / 2.0
    height = (distance(points[0], points[3]) + distance(points[1], points[2])) / 2.0
    if height <= 0.0:
        return None
    aspect = width / height
    expected_digits = 1
    if aspect >= 1.50:
        expected_digits = 3
    elif aspect >= 1.0:
        expected_digits = 2
    return {
        "centerX": center_x,
        "centerY": center_y,
        "width": width,
        "height": height,
        "aspect": aspect,
        "expectedDigits": expected_digits,
        "inSpeedZone": center_x >= 90.0 and center_x <= 235.0 and center_y >= 55.0 and center_y <= 145.0 and width >= 12.0 and width <= 80.0 and height >= 15.0 and height <= 50.0,
    }

def decimal_value(text):
    value = 0
    for index in range(len(text)):
        value = value * 10 + "0123456789".find(text[index])
    return value

def candidate(region):
    shape = geometry(region)
    normalized = normalize_digits(region["text"])
    accepted = shape != None and shape["inSpeedZone"] and normalized != None and len(normalized) > 0
    reason = "CANDIDATE"
    if shape == None or not shape["inSpeedZone"]:
        accepted = False
        reason = "BOX_OUTSIDE_SPEED_GEOMETRY"
    elif region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE:
        accepted = False
        reason = "DETECTION_CONFIDENCE_LOW"
    elif normalized == None or len(normalized) == 0:
        accepted = False
        reason = "TEXT_NOT_NUMERIC"
    elif len(normalized) != shape["expectedDigits"]:
        accepted = False
        reason = "DIGIT_COUNT_DISAGREES_WITH_BOX"
    elif "8" in normalized:
        accepted = False
        reason = "EIGHT_GLYPH_AMBIGUOUS"
    elif region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
        accepted = False
        reason = "RECOGNITION_CONFIDENCE_LOW"
    return {
        "region": region,
        "geometry": shape,
        "normalizedText": normalized,
        "accepted": accepted,
        "reason": "VISUAL_SPEED_CONFIRMED" if accepted else reason,
    }

def main(ctx):
    raw = ctx.inputs
    selected = None
    for region in raw["regions"]:
        current = candidate(region)
        if current["geometry"] == None or not current["geometry"]["inSpeedZone"]:
            continue
        if selected == None or region["detectionConfidence"] > selected["region"]["detectionConfidence"]:
            selected = current

    state = "UNKNOWN"
    display_value = None
    reference_points = None
    reason = "SPEED_BOX_NOT_FOUND"
    raw_text = None
    normalized_text = None
    detection_confidence = 0.0
    recognition_confidence = 0.0
    expected_digits = None
    aspect = None
    if selected != None:
        reason = selected["reason"]
        raw_text = selected["region"]["text"]
        normalized_text = selected["normalizedText"]
        detection_confidence = selected["region"]["detectionConfidence"]
        recognition_confidence = selected["region"]["recognitionConfidence"]
        expected_digits = selected["geometry"]["expectedDigits"]
        aspect = selected["geometry"]["aspect"]
        if selected["accepted"]:
            state = "KNOWN"
            display_value = decimal_value(normalized_text)
            reference_points = selected["region"]["referencePoints"]

    return {
        "schemaVersion": 1,
        "profile": {
            "width": raw["evidence"]["frame"]["width"],
            "height": raw["evidence"]["frame"]["height"],
            "capturedAt": raw["evidence"]["capturedAt"],
        },
        "coordinateSpace": raw["evidence"]["coordinateSpace"],
        "region": raw["evidence"]["referenceRegion"],
        "physicalRegion": raw["evidence"]["physicalRegion"],
        "speed": {
            "state": state,
            "displayValue": display_value,
            "unit": None,
            "referencePoints": reference_points,
            "evidence": {
                "reason": reason,
                "rawText": raw_text,
                "normalizedText": normalized_text,
                "detectionConfidence": detection_confidence,
                "recognitionConfidence": recognition_confidence,
                "minimumDetectionConfidence": MIN_DETECTION_CONFIDENCE,
                "minimumRecognitionConfidence": MIN_RECOGNITION_CONFIDENCE,
                "expectedDigits": expected_digits,
                "boxAspect": aspect,
            },
        },
        "evidence": {"ocrRegionCount": len(raw["regions"]), "models": raw["models"], "timing": raw["timing"]},
    }
