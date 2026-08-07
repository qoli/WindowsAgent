MIN_OCR_CONFIDENCE = 0.55
THRESHOLD_METERS = 7500.0

UNIT_MULTIPLIERS = {
    "M": 1.0,
    "KM": 1000.0,
    "MM": 1000000.0,
    "LS": 299792458.0,
    "LY": 9460730472580800.0,
}

def is_digit(character):
    return character in "0123456789"

def decimal_value(text):
    parts = text.split(".")
    if len(parts) > 2 or len(parts[0]) == 0:
        return None
    integer = 0
    for index in range(len(parts[0])):
        character = parts[0][index]
        if not is_digit(character):
            return None
        integer = integer * 10 + "0123456789".find(character)
    if len(parts) == 1:
        return float(integer)
    if len(parts[1]) == 0:
        return None
    fraction = 0
    denominator = 1
    for index in range(len(parts[1])):
        character = parts[1][index]
        if not is_digit(character):
            return None
        fraction = fraction * 10 + "0123456789".find(character)
        denominator *= 10
    return float(integer) + float(fraction) / float(denominator)

def recognized_unit(text, index):
    remaining = text[index:]
    for unit in ["KM", "MM", "LS", "LY", "M"]:
        if remaining.startswith(unit):
            return unit
    return None

def distance_candidates(text):
    candidates = []
    numeric = ""
    numeric_separated = False
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if is_digit(character):
            if numeric_separated:
                numeric = ""
            numeric += character
            numeric_separated = False
        elif character == ".":
            if numeric_separated:
                numeric = ""
            numeric += character
            numeric_separated = False
        elif character in " \t":
            if len(numeric) > 0:
                numeric_separated = True
        else:
            unit = recognized_unit(upper, index)
            value = decimal_value(numeric)
            if unit != None and value != None:
                candidates.append({
                    "displayText": numeric + unit,
                    "distanceValue": value,
                    "unit": unit,
                    "distanceMeters": value * UNIT_MULTIPLIERS[unit],
                })
            numeric = ""
            numeric_separated = False
    return candidates

def main(ctx):
    raw = ctx.inputs
    candidates = distance_candidates(raw["text"])
    state = "UNKNOWN"
    allowed = None
    selected = None
    reason = "DISTANCE_TEXT_MISSING"
    if raw["confidence"] < MIN_OCR_CONFIDENCE:
        reason = "OCR_CONFIDENCE_LOW"
    elif len(candidates) == 0:
        reason = "DISTANCE_TEXT_INVALID"
    elif len(candidates) > 1:
        reason = "DISTANCE_TEXT_AMBIGUOUS"
    else:
        selected = candidates[0]
        if selected["distanceMeters"] < THRESHOLD_METERS:
            state = "ALLOWED"
            allowed = True
            reason = "DISPLAY_DISTANCE_BELOW_THRESHOLD"
        else:
            state = "DENIED"
            allowed = False
            reason = "DISPLAY_DISTANCE_AT_OR_ABOVE_THRESHOLD"

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
        "requestDockingRange": {
            "state": state,
            "allowed": allowed,
            "displayText": None if selected == None else selected["displayText"],
            "distanceValue": None if selected == None else selected["distanceValue"],
            "unit": None if selected == None else selected["unit"],
            "distanceMeters": None if selected == None else selected["distanceMeters"],
            "thresholdMeters": THRESHOLD_METERS,
            "comparison": "LT",
            "evidence": {
                "reason": reason,
                "rawText": raw["text"],
                "ocrConfidence": raw["confidence"],
                "minimumOcrConfidence": MIN_OCR_CONFIDENCE,
                "candidateCount": len(candidates),
            },
        },
        "evidence": {"model": raw["model"], "timing": raw["timing"]},
    }
