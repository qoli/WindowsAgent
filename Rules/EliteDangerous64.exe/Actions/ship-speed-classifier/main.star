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
    raw = ctx.inputs["ocr"]
    glyph = ctx.inputs["glyph"]
    decoding = raw["decoding"]
    constrained_text = decoding["constrainedText"]
    constrained_confidence = decoding["constrainedConfidence"]
    margin = decoding["rawConstraintMargin"]

    state = "UNKNOWN"
    display_value = None
    raw_candidate = None
    reason = "DIGIT_TEXT_INVALID"
    glyph_state = glyph["zeroGlyph"]["state"]
    ocr_candidate = None
    ocr_reason = None
    if decoding["characterConstraint"] != "digits":
        ocr_reason = "DIGIT_CONSTRAINT_NOT_APPLIED"
    elif not is_digits(constrained_text):
        ocr_reason = "DIGIT_TEXT_INVALID"
    elif constrained_confidence < MIN_CONSTRAINED_CONFIDENCE:
        ocr_reason = "CONSTRAINED_CONFIDENCE_LOW"
    elif margin > MAX_RAW_CONSTRAINT_MARGIN:
        ocr_reason = "RAW_CONSTRAINT_DISAGREEMENT_HIGH"
    else:
        ocr_candidate = decimal_value(constrained_text)

    if glyph_state not in ["ZERO", "NOT_ZERO", "UNKNOWN"]:
        reason = "ZERO_GLYPH_STATE_INVALID"
    elif ocr_candidate != None and ocr_candidate >= 10:
        if glyph_state == "ZERO":
            reason = "MOVING_OCR_ZERO_GLYPH_CONFLICT"
        else:
            state = "MOVING"
            display_value = ocr_candidate
            raw_candidate = ocr_candidate
            reason = "MOVING_SPEED_CONFIRMED"
    elif glyph_state == "ZERO":
        state = "STOPPED"
        display_value = 0
        raw_candidate = 0
        reason = "SLASHED_ZERO_GLYPH_CONFIRMED"
    elif ocr_reason != None:
        reason = ocr_reason
    elif glyph_state == "UNKNOWN":
        reason = "ZERO_GLYPH_EVIDENCE_UNKNOWN"
    elif ocr_candidate == 0:
        reason = "OCR_ZERO_GLYPH_CONFLICT"
    else:
        state = "LOW_SPEED"
        raw_candidate = ocr_candidate
        reason = "LOW_SPEED_RANGE_CONFIRMED"

    return {
        "schemaVersion": 2,
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
            "rawCandidate": raw_candidate,
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
        "evidence": {"model": raw["model"], "timing": raw["timing"], "zeroGlyph": glyph["zeroGlyph"], "glyphProfile": glyph["profile"]},
    }
