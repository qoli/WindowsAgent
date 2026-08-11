MIN_CONSTRAINED_CONFIDENCE = 0.55
MAX_RAW_CONSTRAINT_MARGIN = 0.12
MIN_RAW_PERCENT_CONFIDENCE = 0.80

def is_digits(text):
    if len(text) < 2 or len(text) > 3:
        return False
    for index in range(len(text)):
        character = text[index]
        if character not in "0123456789":
            return False
    return True

def decimal_value(text):
    value = 0
    for index in range(len(text)):
        character = text[index]
        value = value * 10 + "0123456789".find(character)
    return value

def raw_percent_digits(text):
    digits = ""
    for index in range(len(text)):
        character = text[index]
        if character in "0123456789":
            digits += character
        else:
            if character == "%" and len(digits) >= 2 and len(digits) <= 3:
                return digits
            return ""
    return ""

def main(ctx):
    raw = ctx.inputs
    decoding = raw["decoding"]
    text = decoding["constrainedText"]
    confidence = decoding["constrainedConfidence"]
    margin = decoding["rawConstraintMargin"]
    state = "UNKNOWN"
    percent = None
    reason = "DIGIT_TEXT_INVALID"
    raw_digits = raw_percent_digits(decoding["rawText"])
    if raw_digits != "" and decoding["rawConfidence"] >= MIN_RAW_PERCENT_CONFIDENCE:
        candidate = decimal_value(raw_digits)
        if candidate <= 250:
            state = "KNOWN"
            percent = candidate
            reason = "RAW_PERCENT_TEXT_CONFIRMED"
        else:
            reason = "HEAT_PERCENT_OUT_OF_RANGE"
    elif decoding["characterConstraint"] != "digits":
        reason = "DIGIT_CONSTRAINT_NOT_APPLIED"
    elif not is_digits(text):
        reason = "DIGIT_TEXT_INVALID"
    elif confidence < MIN_CONSTRAINED_CONFIDENCE:
        reason = "CONSTRAINED_CONFIDENCE_LOW"
    elif margin > MAX_RAW_CONSTRAINT_MARGIN:
        reason = "RAW_CONSTRAINT_DISAGREEMENT_HIGH"
    else:
        candidate = decimal_value(text)
        if candidate > 250:
            reason = "HEAT_PERCENT_OUT_OF_RANGE"
        else:
            state = "KNOWN"
            percent = candidate
            reason = "HEAT_PERCENT_CONFIRMED"
    return {
        "schemaVersion": 1,
        "heat": {
            "state": state,
            "percent": percent,
            "evidence": {
                "reason": reason,
                "rawText": decoding["rawText"],
                "rawConfidence": decoding["rawConfidence"],
                "constrainedText": text,
                "constrainedConfidence": confidence,
                "rawConstraintMargin": margin,
                "minimumConstrainedConfidence": MIN_CONSTRAINED_CONFIDENCE,
                "maximumRawConstraintMargin": MAX_RAW_CONSTRAINT_MARGIN,
                "minimumRawPercentConfidence": MIN_RAW_PERCENT_CONFIDENCE,
            },
        },
        "profile": {"width": raw["evidence"]["frame"]["width"], "height": raw["evidence"]["frame"]["height"], "capturedAt": raw["evidence"]["capturedAt"]},
        "coordinateSpace": raw["evidence"]["coordinateSpace"],
        "region": raw["evidence"]["referenceRegion"],
        "physicalRegion": raw["evidence"]["physicalRegion"],
        "timing": raw["timing"],
    }
