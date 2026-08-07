MIN_CONSTRAINED_CONFIDENCE = 0.55
MAX_RAW_CONSTRAINT_MARGIN = 0.12
MAX_DIGITS = 4

def is_digits(text):
    if len(text) == 0 or len(text) > MAX_DIGITS:
        return False
    for index in range(len(text)):
        if text[index] not in "0123456789":
            return False
    return True

def decimal_value(text):
    value = 0
    for index in range(len(text)):
        value = value * 10 + "0123456789".find(text[index])
    return value

def main(ctx):
    raw = ctx.inputs
    decoding = raw["decoding"]
    constrained_text = decoding["constrainedText"]
    constrained_confidence = decoding["constrainedConfidence"]
    margin = decoding["rawConstraintMargin"]

    state = "UNKNOWN"
    display_value = None
    reason = "DIGIT_TEXT_INVALID"
    if decoding["characterConstraint"] != "digits":
        reason = "DIGIT_CONSTRAINT_NOT_APPLIED"
    elif not is_digits(constrained_text):
        reason = "DIGIT_TEXT_INVALID"
    elif constrained_confidence < MIN_CONSTRAINED_CONFIDENCE:
        reason = "CONSTRAINED_CONFIDENCE_LOW"
    elif margin > MAX_RAW_CONSTRAINT_MARGIN:
        reason = "RAW_CONSTRAINT_DISAGREEMENT_HIGH"
    else:
        state = "KNOWN"
        display_value = decimal_value(constrained_text)
        reason = "VISUAL_SPEED_CONFIRMED"

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
            "referencePoints": None,
            "evidence": {
                "reason": reason,
                "rawText": decoding["rawText"],
                "rawConfidence": decoding["rawConfidence"],
                "constrainedText": constrained_text,
                "constrainedConfidence": constrained_confidence,
                "rawConstraintMargin": margin,
                "minimumConstrainedConfidence": MIN_CONSTRAINED_CONFIDENCE,
                "maximumRawConstraintMargin": MAX_RAW_CONSTRAINT_MARGIN,
            },
        },
        "evidence": {"model": raw["model"], "timing": raw["timing"]},
    }
