MIN_DETECTION_CONFIDENCE = 0.70
MIN_RECOGNITION_CONFIDENCE = 0.75
IDENTITY_CORROBORATED_PREFIX_MIN_DETECTION_CONFIDENCE = 0.55
IDENTITY_CORROBORATED_PREFIX_MIN_RECOGNITION_CONFIDENCE = 0.90
SCREEN_CENTER_X = 960.0
SCREEN_CENTER_Y = 540.0
LABEL_TO_MARKER_X = 30.0
# The destination ring is below the OCR label centre. The subtraction at the
# call sites therefore uses a negative offset to move the derived point down.
LABEL_TO_MARKER_Y = -12.5
DUPLICATE_BOX_TOLERANCE_PIXELS = 16.0
MIN_FOCUS_FRAME_CONFIDENCE_PERMILLE = 650
MIN_SHAPE_CONFIDENCE_PERMILLE = 520
MIN_LAYOUT_CONFIDENCE_PERMILLE = 400
MIN_FOCUS_FRAME_LEAD_PERMILLE = 30
MIN_OCCLUDED_WORD_PREFIX = 4
MULTILINE_LEFT_TOLERANCE_PIXELS = 16.0
MULTILINE_MIN_CENTER_GAP_PIXELS = 12.0
MULTILINE_MAX_CENTER_GAP_PIXELS = 36.0
SAME_LINE_CENTER_TOLERANCE_PIXELS = 20.0
SAME_LINE_MAX_HORIZONTAL_GAP_PIXELS = 120.0
SAME_LINE_MAX_HORIZONTAL_OVERLAP_PIXELS = 20.0

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

def same_line_occluded_two_words(actual_text, expected_words):
    actual_words = normalized_words(actual_text)
    if len(actual_words) != 2 or len(expected_words) != 2:
        return False
    actual_name = actual_words[0]
    expected_name = expected_words[0]
    # A cockpit pillar can remove the middle of a Station's proper name while
    # leaving its first and last letters on one OCR line. Keep this narrow: the
    # proper-name fragment must contain both ordered endpoints, and the complete
    # type word must independently pass the normal one-edit matcher.
    if len(actual_name) < 2 or len(expected_name) < 4:
        return False
    if actual_name[0] != expected_name[0] or actual_name[-1] != expected_name[-1]:
        return False
    previous = -1
    for actual_index in range(len(actual_name)):
        character = actual_name[actual_index]
        found = False
        for index in range(previous + 1, len(expected_name)):
            if expected_name[index] == character:
                previous = index
                found = True
                break
        if not found:
            return False
    return one_edit_or_exact(actual_words[1], expected_words[1])

def same_line_identity_corroborated_fragment(actual_text, expected_words, identity_confirmed):
    if not identity_confirmed:
        return False
    actual_words = normalized_words(actual_text)
    if len(actual_words) != 2 or len(expected_words) != 2:
        return False
    fragment = actual_words[0]
    expected_name = expected_words[0]
    if len(fragment) == 0 or not one_edit_or_exact(actual_words[1], expected_words[1]):
        return False
    previous = -1
    for fragment_index in range(len(fragment)):
        character = fragment[fragment_index]
        found = False
        for expected_index in range(previous + 1, len(expected_name)):
            if expected_name[expected_index] == character:
                previous = expected_index
                found = True
                break
        if not found:
            return False
    return True

def identity_corroborated_fused_pillar_label(actual_text, expected_words, identity_confirmed):
    if not identity_confirmed or len(expected_words) != 2:
        return False
    actual = normalize(actual_text)
    expected_name = expected_words[0]
    expected_type = expected_words[1]
    if len(actual) < 7 or len(expected_name) < 4 or actual[:3] != expected_name[:3]:
        return False
    for split_index in range(3, len(actual)):
        type_fragment = actual[split_index:]
        if one_edit_or_exact(type_fragment, expected_type):
            return True
    return False

def identity_corroborated_proper_name_prefix(actual, expected, identity_confirmed):
    if not identity_confirmed or len(actual) < 3 or len(actual) > len(expected):
        return False
    return actual == expected[:len(actual)]

def identity_corroborated_type_suffix(actual, expected, identity_confirmed):
    if not identity_confirmed or len(actual) < 4 or len(actual) > len(expected):
        return False
    return actual == expected[len(expected) - len(actual):]

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

def stacked_full_name_candidate(first, second):
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
        "matchReason": "STACKED_FULL_TARGET_NAME_CONFIRMED",
    }

def same_line_split_candidate(first, second):
    first_box = bounds(first["referencePoints"])
    second_box = bounds(second["referencePoints"])
    confidence = first["recognitionConfidence"]
    if second["recognitionConfidence"] < confidence:
        confidence = second["recognitionConfidence"]
    return {
        "text": first["text"] + " " + second["text"],
        "detectionConfidence": first["detectionConfidence"],
        "recognitionConfidence": confidence,
        "referencePoints": [
            {"x": first_box["left"], "y": first_box["top"]},
            {"x": second_box["right"], "y": first_box["top"]},
            {"x": second_box["right"], "y": second_box["bottom"]},
            {"x": first_box["left"], "y": second_box["bottom"]},
        ],
        "matchReason": "PILLAR_SPLIT_SAME_LINE_WORDS_CONFIRMED",
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

def same_box(first, second):
    first_box = bounds(first["referencePoints"])
    second_box = bounds(second["referencePoints"])
    return abs(first_box["left"] - second_box["left"]) <= DUPLICATE_BOX_TOLERANCE_PIXELS and abs(first_box["centerY"] - second_box["centerY"]) <= DUPLICATE_BOX_TOLERANCE_PIXELS

def square_root(value):
    if value <= 0:
        return 0.0
    guess = value
    for _ in range(12):
        guess = (guess + value / guess) / 2.0
    return guess

def locate_reticle(region):
    box = bounds(region["referencePoints"])
    hint_x = int(box["left"] - LABEL_TO_MARKER_X)
    hint_y = int(box["centerY"] - LABEL_TO_MARKER_Y)
    if hint_x < 70:
        hint_x = 70
    elif hint_x > 1850:
        hint_x = 1850
    if hint_y < 70:
        hint_y = 70
    elif hint_y > 1010:
        hint_y = 1010
    return action.call(id="elite-dangerous/supercruise-visible-reticle-position", inputs={"hintX": hint_x, "hintY": hint_y})

def clamp_permille(value):
    if value < 0:
        return 0
    if value > 1000:
        return 1000
    return value

def focus_frame_candidate(region, reticle_output, identity_confirmed):
    reticle = reticle_output["target"]
    if reticle["state"] != "DETECTED":
        return None
    box = bounds(region["referencePoints"])
    horizontal_gap = box["left"] - reticle["referenceX"]
    vertical_gap = reticle["referenceY"] - box["centerY"]
    horizontal_score = clamp_permille(1000 - int(abs(horizontal_gap - LABEL_TO_MARKER_X) * 20.0))
    vertical_score = clamp_permille(1000 - int(abs(vertical_gap + LABEL_TO_MARKER_Y) * 25.0))
    layout_confidence = (horizontal_score * 60 + vertical_score * 40) // 100
    text_confidence = int(min(region["detectionConfidence"], region["recognitionConfidence"]) * 1000.0)
    shape_confidence = reticle["shapeConfidencePermille"]
    # Shape owns candidate admission and most of the fused score. Text may
    # confirm identity and layout, but cannot rescue an invalid focus frame.
    focus_confidence = (shape_confidence * 60 + layout_confidence * 25 + text_confidence * 15) // 100
    if shape_confidence < MIN_SHAPE_CONFIDENCE_PERMILLE or layout_confidence < MIN_LAYOUT_CONFIDENCE_PERMILLE or focus_confidence < MIN_FOCUS_FRAME_CONFIDENCE_PERMILLE:
        return None
    return {
        "region": region,
        "reticleOutput": reticle_output,
        "shapeConfidencePermille": shape_confidence,
        "layoutConfidencePermille": layout_confidence,
        "textConfidencePermille": text_confidence,
        "focusFrameConfidencePermille": focus_confidence,
        "horizontalGapPixels": horizontal_gap,
        "verticalGapPixels": vertical_gap,
        "identityConfirmed": identity_confirmed,
    }

def append_focus_candidate(candidates, candidate):
    candidate_box = bounds(candidate["region"]["referencePoints"])
    candidate_reticle = candidate["reticleOutput"]["target"]
    for index in range(len(candidates)):
        existing = candidates[index]
        existing_box = bounds(existing["region"]["referencePoints"])
        existing_reticle = existing["reticleOutput"]["target"]
        if abs(candidate_box["left"] - existing_box["left"]) <= DUPLICATE_BOX_TOLERANCE_PIXELS and abs(candidate_box["centerY"] - existing_box["centerY"]) <= DUPLICATE_BOX_TOLERANCE_PIXELS and abs(candidate_reticle["referenceX"] - existing_reticle["referenceX"]) <= 20 and abs(candidate_reticle["referenceY"] - existing_reticle["referenceY"]) <= 20:
            if candidate["focusFrameConfidencePermille"] > existing["focusFrameConfidencePermille"]:
                candidates[index] = candidate
            return
    candidates.append(candidate)

def unknown_target(reason, raw_texts, captured_at = None):
    return {
        "state": "UNKNOWN", "referenceX": None, "referenceY": None,
        "offsetX": None, "offsetY": None, "centerDistancePixels": None,
        "reason": reason, "presentation": None, "occupiedAngularBins": None,
        "angularRuns": None, "reticleEvidencePlane": None,
        "reticleEvidenceQuality": None, "reticleCapturedAt": captured_at,
        "shapeConfidencePermille": None, "layoutConfidencePermille": None,
        "textConfidencePermille": None, "focusFrameConfidencePermille": None,
        "identityConfirmed": False, "horizontalGapPixels": None,
        "verticalGapPixels": None, "rawTexts": raw_texts,
    }

def main(ctx):
    target_name = ctx.inputs["targetName"]
    expected = normalize(target_name)
    expected_words = normalized_words(target_name)
    bands = [
        action.call(id="elite-dangerous/supercruise-target-text-regions", inputs={}),
        action.call(id="elite-dangerous/supercruise-target-text-regions-lower", inputs={}),
        action.call(id="elite-dangerous/supercruise-target-text-regions-lower-wide", inputs={}),
        action.call(id="elite-dangerous/supercruise-target-text-regions-upper-left", inputs={}),
        action.call(id="elite-dangerous/supercruise-target-text-regions-upper-right", inputs={}),
    ]
    identity_band = action.call(id="elite-dangerous/request-docking-distance-regions", inputs={})
    identity_confirmed = False
    for identity_region in identity_band["regions"]:
        if identity_region["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and identity_region["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and one_edit_or_exact(normalize(identity_region["text"]), expected):
            identity_confirmed = True
            break
    matches = []
    proposals = []
    raw_texts = []
    for raw in bands:
        eligible_regions = []
        corroborated_split_regions = []
        for region in raw["regions"]:
            raw_texts.append(region["text"])
            if identity_confirmed and region["detectionConfidence"] >= IDENTITY_CORROBORATED_PREFIX_MIN_DETECTION_CONFIDENCE and region["recognitionConfidence"] >= IDENTITY_CORROBORATED_PREFIX_MIN_RECOGNITION_CONFIDENCE:
                corroborated_split_regions.append(region)
            if region["detectionConfidence"] < MIN_DETECTION_CONFIDENCE or region["recognitionConfidence"] < MIN_RECOGNITION_CONFIDENCE:
                continue
            eligible_regions.append(region)
            # OCR supplies a spatial proposal, not a semantic Gate. Every
            # normally legible forward-HUD box receives shape analysis before
            # target-name and layout evidence are allowed to select it.
            append_deduplicated(proposals, region)
            exact_or_one_edit = one_edit_or_exact(normalize(region["text"]), expected)
            occluded_same_line = same_line_occluded_two_words(region["text"], expected_words)
            identity_fragment = same_line_identity_corroborated_fragment(region["text"], expected_words, identity_confirmed)
            fused_pillar = identity_corroborated_fused_pillar_label(region["text"], expected_words, identity_confirmed)
            normalized_region = normalize(region["text"])
            identity_prefix_hint = identity_confirmed and len(expected_words) == 2 and len(normalized_region) >= 3 and normalized_region[:3] == expected_words[0][:3]
            if not exact_or_one_edit and not occluded_same_line and not identity_fragment and not fused_pillar and not identity_prefix_hint:
                continue
            if identity_prefix_hint and not fused_pillar and not identity_fragment and not occluded_same_line:
                region = {
                    "text": region["text"],
                    "detectionConfidence": region["detectionConfidence"],
                    "recognitionConfidence": region["recognitionConfidence"],
                    "referencePoints": region["referencePoints"],
                    "matchReason": "EXACT_SELECTED_IDENTITY_AND_PROPER_NAME_PREFIX_SEARCH_HINT_CONFIRMED",
                }
            elif fused_pillar:
                region = {
                    "text": region["text"],
                    "detectionConfidence": region["detectionConfidence"],
                    "recognitionConfidence": region["recognitionConfidence"],
                    "referencePoints": region["referencePoints"],
                    "matchReason": "PILLAR_FUSED_POSITION_AND_EXACT_SELECTED_IDENTITY_CONFIRMED",
                }
            elif identity_fragment and not occluded_same_line:
                region = {
                    "text": region["text"],
                    "detectionConfidence": region["detectionConfidence"],
                    "recognitionConfidence": region["recognitionConfidence"],
                    "referencePoints": region["referencePoints"],
                    "matchReason": "OCCLUDED_POSITION_FRAGMENT_AND_EXACT_SELECTED_IDENTITY_CONFIRMED",
                }
            elif occluded_same_line:
                region = {
                    "text": region["text"],
                    "detectionConfidence": region["detectionConfidence"],
                    "recognitionConfidence": region["recognitionConfidence"],
                    "referencePoints": region["referencePoints"],
                    "matchReason": "OCCLUDED_SAME_LINE_PROPER_NAME_ENDPOINTS_CONFIRMED",
                }
            append_deduplicated(matches, region)
        # Long System names are rendered as two stacked HUD lines even though
        # they contain more than two words. Combine only same-frame boxes that
        # share the reviewed left edge and line spacing, and require their
        # complete normalized concatenation to match the requested target.
        for first in eligible_regions:
            first_box = bounds(first["referencePoints"])
            for second in eligible_regions:
                if second == first:
                    continue
                second_box = bounds(second["referencePoints"])
                center_gap = second_box["centerY"] - first_box["centerY"]
                if abs(second_box["left"] - first_box["left"]) > MULTILINE_LEFT_TOLERANCE_PIXELS or center_gap < MULTILINE_MIN_CENTER_GAP_PIXELS or center_gap > MULTILINE_MAX_CENTER_GAP_PIXELS:
                    continue
                if one_edit_or_exact(normalize(first["text"] + " " + second["text"]), expected):
                    combined = stacked_full_name_candidate(first, second)
                    proposals.append(combined)
                    append_deduplicated(matches, combined)
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
                    combined = multiline_candidate(first, second)
                    proposals.append(combined)
                    append_deduplicated(matches, combined)
            split_regions = corroborated_split_regions if identity_confirmed else eligible_regions
            for first in split_regions:
                first_normalized = normalize(first["text"])
                if not occluded_word_prefix_matches(first_normalized, expected_words[0]) and not identity_corroborated_proper_name_prefix(first_normalized, expected_words[0], identity_confirmed):
                    continue
                first_box = bounds(first["referencePoints"])
                for second in split_regions:
                    second_normalized = normalize(second["text"])
                    if second == first or (not one_edit_or_exact(second_normalized, expected_words[1]) and not identity_corroborated_type_suffix(second_normalized, expected_words[1], identity_confirmed)):
                        continue
                    second_box = bounds(second["referencePoints"])
                    horizontal_gap = second_box["left"] - first_box["right"]
                    if horizontal_gap < -SAME_LINE_MAX_HORIZONTAL_OVERLAP_PIXELS or horizontal_gap > SAME_LINE_MAX_HORIZONTAL_GAP_PIXELS or abs(second_box["centerY"] - first_box["centerY"]) > SAME_LINE_CENTER_TOLERANCE_PIXELS:
                        continue
                    combined = same_line_split_candidate(first, second)
                    proposals.append(combined)
                    append_deduplicated(matches, combined)
    shaped_proposals = []
    last_reticle_capture = None
    for proposal in proposals:
        reticle_output = locate_reticle(proposal)
        last_reticle_capture = reticle_output["evidence"]["capturedAt"]
        if reticle_output["target"]["state"] == "DETECTED":
            shaped_proposals.append({"region": proposal, "reticleOutput": reticle_output})
    if len(matches) == 0:
        return {
            "schemaVersion": 1,
            "target": unknown_target("TARGET_TEXT_NOT_FOUND", raw_texts),
            "timing": {"bands": [bands[0]["timing"], bands[1]["timing"], bands[2]["timing"], bands[3]["timing"], bands[4]["timing"]], "identity": identity_band["timing"]},
        }
    candidates = []
    for match in matches:
        for shaped in shaped_proposals:
            if same_box(match, shaped["region"]):
                candidate = focus_frame_candidate(match, shaped["reticleOutput"], identity_confirmed)
                if candidate != None:
                    append_focus_candidate(candidates, candidate)
    if len(candidates) == 0:
        return {
            "schemaVersion": 1,
            "target": unknown_target("NO_SHAPE_FIRST_FOCUS_FRAME_CANDIDATE", raw_texts, last_reticle_capture),
            "timing": {"bands": [bands[0]["timing"], bands[1]["timing"], bands[2]["timing"], bands[3]["timing"], bands[4]["timing"]], "identity": identity_band["timing"]},
        }
    selected = candidates[0]
    runner_up = None
    for candidate in candidates[1:]:
        if candidate["focusFrameConfidencePermille"] > selected["focusFrameConfidencePermille"]:
            runner_up = selected
            selected = candidate
        elif runner_up == None or candidate["focusFrameConfidencePermille"] > runner_up["focusFrameConfidencePermille"]:
            runner_up = candidate
    if runner_up != None and selected["focusFrameConfidencePermille"] - runner_up["focusFrameConfidencePermille"] < MIN_FOCUS_FRAME_LEAD_PERMILLE:
        return {
            "schemaVersion": 1,
            "target": unknown_target("FOCUS_FRAME_CANDIDATES_AMBIGUOUS", raw_texts, selected["reticleOutput"]["evidence"]["capturedAt"]),
            "timing": {"bands": [bands[0]["timing"], bands[1]["timing"], bands[2]["timing"], bands[3]["timing"], bands[4]["timing"]], "identity": identity_band["timing"]},
        }
    region = selected["region"]
    reticle_output = selected["reticleOutput"]
    reticle = reticle_output["target"]
    reason = region.get("matchReason", "TARGET_LABEL_TO_MARKER_OFFSET_APPLIED")
    if len(matches) > 1:
        reason = "HIGHEST_SHAPE_LAYOUT_TEXT_CONFIDENCE_SELECTED"
    reference_x = reticle["referenceX"]
    reference_y = reticle["referenceY"]
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
            "reason": reason + ":ORANGE_RETICLE_ANNULUS_CENTER_CONFIRMED",
            "presentation": reticle["presentation"],
            "occupiedAngularBins": reticle["occupiedAngularBins"],
            "angularRuns": reticle["angularRuns"],
            "reticleEvidencePlane": reticle["evidencePlane"],
            "reticleEvidenceQuality": reticle["evidenceQuality"],
            "reticleCapturedAt": reticle_output["evidence"]["capturedAt"],
            "shapeConfidencePermille": selected["shapeConfidencePermille"],
            "layoutConfidencePermille": selected["layoutConfidencePermille"],
            "textConfidencePermille": selected["textConfidencePermille"],
            "focusFrameConfidencePermille": selected["focusFrameConfidencePermille"],
            "identityConfirmed": selected["identityConfirmed"],
            "horizontalGapPixels": selected["horizontalGapPixels"],
            "verticalGapPixels": selected["verticalGapPixels"],
            "rawTexts": raw_texts,
        },
        "timing": {"bands": [bands[0]["timing"], bands[1]["timing"], bands[2]["timing"], bands[3]["timing"], bands[4]["timing"]], "identity": identity_band["timing"]},
    }
