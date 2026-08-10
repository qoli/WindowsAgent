MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.50
MIN_TEXT_SIMILARITY = 0.70
MIN_TEXT_MARGIN = 0.12
AMBIGUOUS_TEXT_SIMILARITY = 0.45
MIN_ANCHOR_SIMILARITY = 0.75
MIN_ANCHOR_MARGIN = 0.12
MIN_DENIAL_SIMILARITY = 0.80
MIN_ACTION_VERTICAL_OFFSET = 4.0
MAX_ACTION_VERTICAL_OFFSET = 170.0
MIN_ACTION_HORIZONTAL_OFFSET = -96.0
MAX_ACTION_HORIZONTAL_OFFSET = 320.0
# Two live 4K/HDR captures of the settled focused Request Docking row produced
# bright ratios of 0.0892 and 0.0934. Keep the threshold below that reviewed
# band while leaving a measurable gap above weak 0.07 evidence.
FOCUSED_BRIGHT_MINIMUM = 0.08
VISIBLE_DARK_MINIMUM = 0.08
OUTPUT_SCALE = 10000.0

def normalize_text(text):
    normalized = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            normalized += character
    return normalized

def edit_distance(left, right):
    previous = []
    for index in range(len(right) + 1):
        previous.append(index)
    for left_index in range(len(left)):
        current = [left_index + 1]
        for right_index in range(len(right)):
            insertion = current[right_index] + 1
            deletion = previous[right_index + 1] + 1
            substitution = previous[right_index]
            if left[left_index] != right[right_index]:
                substitution += 1
            best = insertion
            if deletion < best:
                best = deletion
            if substitution < best:
                best = substitution
            current.append(best)
        previous = current
    return previous[len(right)]

def similarity(left, right):
    maximum = len(left)
    if len(right) > maximum:
        maximum = len(right)
    if maximum == 0:
        return 0.0
    return 1.0 - float(edit_distance(left, right)) / float(maximum)

def candidate_for(region, label, expected):
    normalized = normalize_text(region["text"])
    text_similarity = similarity(normalized, expected)
    return {
        "label": label,
        "region": region,
        "normalizedText": normalized,
        "similarity": text_similarity,
        "score": region["recognitionConfidence"] * text_similarity,
    }

def reference_bounds(region):
    points = region["referencePoints"]
    if len(points) != 4:
        return None
    minimum_x = points[0]["x"]
    maximum_x = points[0]["x"]
    minimum_y = points[0]["y"]
    maximum_y = points[0]["y"]
    for point in points[1:]:
        if point["x"] < minimum_x:
            minimum_x = point["x"]
        if point["x"] > maximum_x:
            maximum_x = point["x"]
        if point["y"] < minimum_y:
            minimum_y = point["y"]
        if point["y"] > maximum_y:
            maximum_y = point["y"]
    if maximum_x <= minimum_x or maximum_y <= minimum_y:
        return None
    return {"left": minimum_x, "top": minimum_y, "right": maximum_x, "bottom": maximum_y}

def select_candidate(regions, label, expected, anchor_bounds=None):
    best = None
    for region in regions:
        bounds = reference_bounds(region)
        if bounds == None:
            continue
        if anchor_bounds != None:
            horizontal_offset = bounds["left"] - anchor_bounds["left"]
            vertical_offset = bounds["top"] - anchor_bounds["bottom"]
            if horizontal_offset < MIN_ACTION_HORIZONTAL_OFFSET or horizontal_offset > MAX_ACTION_HORIZONTAL_OFFSET:
                continue
            if vertical_offset < MIN_ACTION_VERTICAL_OFFSET or vertical_offset > MAX_ACTION_VERTICAL_OFFSET:
                continue
        candidate = candidate_for(region, label, expected)
        candidate["bounds"] = bounds
        if best == None or candidate["score"] > best["score"]:
            best = candidate
    return best

def select_anchor(regions):
    best = None
    runner_up = None
    for region in regions:
        candidate = candidate_for(region, "FACTION", "FACTION")
        bounds = reference_bounds(region)
        if bounds == None:
            continue
        candidate["bounds"] = bounds
        if best == None or candidate["score"] > best["score"]:
            runner_up = best
            best = candidate
        elif runner_up == None or candidate["score"] > runner_up["score"]:
            runner_up = candidate
    runner_score = 0.0 if runner_up == None else runner_up["score"]
    margin = 0.0 if best == None else best["score"] - runner_score
    accepted = best != None and best["region"]["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and best["region"]["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and best["similarity"] >= MIN_ANCHOR_SIMILARITY and margin >= MIN_ANCHOR_MARGIN
    return best, accepted, margin

def select_candidates(regions, anchor_bounds):
    request = select_candidate(regions, "REQUEST", "REQUESTDOCKING", anchor_bounds=anchor_bounds)
    cancel = select_candidate(regions, "CANCEL", "CANCELDOCKING", anchor_bounds=anchor_bounds)
    if request == None or (cancel != None and cancel["score"] > request["score"]):
        return cancel, request
    return request, cancel

def select_denial(regions):
    return select_candidate(regions, "DENIED", "DOCKINGREQUESTDENIED")

def pixel_channels(pixel):
    return pixel // 65536, (pixel // 256) % 256, pixel % 256

def is_bright(red, green, blue):
    return red >= 190 and green >= 110 and blue <= 100 and red >= green + 25

def is_dark_fill(red, green, blue):
    return red >= 65 and green >= 15 and green <= 90 and blue <= 45 and red >= green + 20

def classify_visual(region):
    context = region["leftContext"]
    pixels = context["pixels"]
    expected = context["w"] * context["h"]
    if expected <= 0 or len(pixels) != expected:
        return {"state": "UNKNOWN", "focused": None, "brightRatio": 0.0, "darkFillRatio": 0.0, "reason": "LEFT_CONTEXT_INVALID"}
    bright = 0
    dark = 0
    for pixel in pixels:
        red, green, blue = pixel_channels(pixel)
        if is_bright(red, green, blue):
            bright += 1
        if is_dark_fill(red, green, blue):
            dark += 1
    bright_ratio = math.round(float(bright) / float(expected) * OUTPUT_SCALE) / OUTPUT_SCALE
    dark_ratio = math.round(float(dark) / float(expected) * OUTPUT_SCALE) / OUTPUT_SCALE
    if bright_ratio >= FOCUSED_BRIGHT_MINIMUM:
        return {"state": "FOCUSED", "focused": True, "brightRatio": bright_ratio, "darkFillRatio": dark_ratio, "reason": "DYNAMIC_LEFT_CONTEXT_BRIGHT"}
    if dark_ratio >= VISIBLE_DARK_MINIMUM:
        return {"state": "VISIBLE", "focused": False, "brightRatio": bright_ratio, "darkFillRatio": dark_ratio, "reason": "DYNAMIC_LEFT_CONTEXT_DARK"}
    return {"state": "UNKNOWN", "focused": None, "brightRatio": bright_ratio, "darkFillRatio": dark_ratio, "reason": "DYNAMIC_LEFT_CONTEXT_AMBIGUOUS"}

def meaningful_region_count(regions):
    count = 0
    for region in regions:
        if region["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and region["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and len(normalize_text(region["text"])) > 0:
            count += 1
    return count

def unknown_result(contacts, reason):
    return {
        "schemaVersion": 1,
        "requestDocking": {"state": "UNKNOWN", "available": None, "focused": None},
        "source": {"activeTab": contacts["activeTab"], "searchRegion": None, "ocrTiming": None, "regionCount": None, "anchor": None, "text": None, "normalizedText": None, "referencePoints": None, "leftContextRegion": None, "visual": None},
        "decision": {"accepted": False, "candidate": None, "candidateScore": 0.0, "candidateSimilarity": 0.0, "margin": 0.0, "reason": reason},
    }

def evidence_unknown_result(contacts, raw, reason, anchor, anchor_margin, candidate=None):
    regions = raw["regions"]
    anchor_source = None
    if anchor != None:
        anchor_source = {
            "text": anchor["region"]["text"],
            "normalizedText": anchor["normalizedText"],
            "referencePoints": anchor["region"]["referencePoints"],
            "bounds": anchor["bounds"],
            "similarity": anchor["similarity"],
            "margin": anchor_margin,
        }
    return {
        "schemaVersion": 1,
        "requestDocking": {"state": "UNKNOWN", "available": None, "focused": None},
        "source": {
            "activeTab": contacts["activeTab"],
            "searchRegion": raw["evidence"]["referenceRegion"],
            "ocrTiming": raw["timing"],
            "regionCount": len(regions),
            "anchor": anchor_source,
            "text": None if candidate == None else candidate["region"]["text"],
            "normalizedText": None if candidate == None else candidate["normalizedText"],
            "referencePoints": None if candidate == None else candidate["region"]["referencePoints"],
            "leftContextRegion": None if candidate == None else candidate["region"]["leftContext"]["referenceRegion"],
            "visual": None,
        },
        "decision": {
            "accepted": False,
            "candidate": None if candidate == None else candidate["label"],
            "candidateScore": 0.0 if candidate == None else candidate["score"],
            "candidateSimilarity": 0.0 if candidate == None else candidate["similarity"],
            "margin": 0.0,
            "anchorMargin": anchor_margin,
            "reason": reason,
        },
    }

def denied_result(contacts, raw, denial, anchor, anchor_accepted, anchor_margin):
    anchor_source = None
    if anchor != None and anchor_accepted:
        anchor_source = {
            "text": anchor["region"]["text"],
            "normalizedText": anchor["normalizedText"],
            "referencePoints": anchor["region"]["referencePoints"],
            "bounds": anchor["bounds"],
            "similarity": anchor["similarity"],
            "margin": anchor_margin,
        }
    return {
        "schemaVersion": 1,
        # The notification proves rejection, but it does not prove the
        # post-submit button's current availability or focus state.
        "requestDocking": {"state": "DENIED", "available": None, "focused": None},
        "source": {
            "activeTab": contacts["activeTab"],
            "searchRegion": raw["evidence"]["referenceRegion"],
            "ocrTiming": raw["timing"],
            "regionCount": len(raw["regions"]),
            "anchor": anchor_source,
            "text": denial["region"]["text"],
            "normalizedText": denial["normalizedText"],
            "referencePoints": denial["region"]["referencePoints"],
            "leftContextRegion": denial["region"]["leftContext"]["referenceRegion"],
            "visual": None,
        },
        "decision": {
            "accepted": True,
            "candidate": "DENIED",
            "candidateScore": denial["score"],
            "candidateSimilarity": denial["similarity"],
            "margin": 0.0,
            "minimumDetectionConfidence": MIN_DETECTION_CONFIDENCE,
            "minimumRecognitionConfidence": MIN_RECOGNITION_CONFIDENCE,
            "minimumDenialSimilarity": MIN_DENIAL_SIMILARITY,
            "reason": "DOCKING_REQUEST_DENIED_CONFIRMED",
        },
    }

def main(ctx):
    contacts = ctx.inputs["contacts"]
    contacts_state = contacts["activeTab"]["state"]
    if contacts_state not in ["CONTACTS", "UNKNOWN"]:
        return unknown_result(contacts, "CONTACTS_TAB_NOT_SELECTED")
    raw = ctx.inputs["regions"]
    if raw == None:
        return unknown_result(contacts, "TEXT_REGIONS_MISSING")
    regions = raw["regions"]
    meaningful_count = meaningful_region_count(regions)
    anchor, anchor_accepted, anchor_margin = select_anchor(regions)
    denial = select_denial(regions)
    denial_accepted = denial != None and denial["region"]["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and denial["region"]["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and denial["similarity"] >= MIN_DENIAL_SIMILARITY
    denial_overridden = False
    if denial_accepted and anchor_accepted:
        conflict_best, conflict_runner_up = select_candidates(regions, anchor["bounds"])
        conflict_runner_score = 0.0 if conflict_runner_up == None else conflict_runner_up["score"]
        conflict_margin = 0.0 if conflict_best == None else conflict_best["score"] - conflict_runner_score
        conflict_accepted = conflict_best != None and conflict_best["region"]["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and conflict_best["region"]["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and conflict_best["similarity"] >= MIN_TEXT_SIMILARITY and conflict_margin >= MIN_TEXT_MARGIN
        denial_overridden = conflict_accepted and conflict_best["label"] == "CANCEL"
    # The complete denial notification is a distinctive, terminal response to
    # SELECT. It is accepted anywhere inside this Action's already-bounded
    # Request Docking ROI, even if the short-lived frame does not also yield a
    # stable FACTION anchor or tab probe.
    if denial_accepted and not denial_overridden:
        return denied_result(contacts, raw, denial, anchor, anchor_accepted, anchor_margin)
    if not anchor_accepted:
        return evidence_unknown_result(contacts, raw, "CONTACTS_ACTION_ANCHOR_NOT_CONFIRMED", anchor, anchor_margin)
    best, runner_up = select_candidates(regions, anchor["bounds"])
    unbounded_best, unused_unbounded_runner = select_candidates(regions, None)
    if best == None and unbounded_best != None and unbounded_best["similarity"] >= AMBIGUOUS_TEXT_SIMILARITY:
        return evidence_unknown_result(contacts, raw, "ACTION_TEXT_OUTSIDE_ANCHORED_ZONE", anchor, anchor_margin, candidate=unbounded_best)
    runner_score = 0.0 if runner_up == None else runner_up["score"]
    margin = 0.0 if best == None else best["score"] - runner_score
    accepted = best != None and best["region"]["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and best["region"]["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and best["similarity"] >= MIN_TEXT_SIMILARITY and margin >= MIN_TEXT_MARGIN

    # Once Auto Dock begins moving the cockpit, the fixed tab probe can be
    # transiently UNKNOWN even though the dynamic OCR box still confirms
    # CANCEL DOCKING. Only that one-way post-submit evidence is admitted while
    # the tab is UNKNOWN; REQUEST and all weaker candidates remain UNKNOWN.
    if contacts_state == "UNKNOWN" and (not accepted or best["label"] != "CANCEL"):
        return unknown_result(contacts, "CONTACTS_TAB_NOT_CONFIRMED")

    state = "UNKNOWN"
    available = None
    focused = None
    reason = "ACTION_TEXT_AMBIGUOUS"
    visual = None
    if (best == None or best["similarity"] < AMBIGUOUS_TEXT_SIMILARITY) and meaningful_count > 0:
        state = "UNAVAILABLE"
        available = False
        focused = False
        reason = "ACTION_TEXT_NOT_DETECTED"
    elif best == None or best["similarity"] < AMBIGUOUS_TEXT_SIMILARITY:
        reason = "ACTION_AREA_NOT_CONFIRMED"
    elif accepted:
        visual = classify_visual(best["region"])
        if best["label"] == "CANCEL":
            state = "DOCKING_ACTIVE"
            available = False
            focused = visual["focused"]
            reason = "CANCEL_DOCKING_OVERRIDES_DENIAL_NOTIFICATION" if denial_overridden else "CANCEL_DOCKING_CONFIRMED"
        elif visual["state"] == "FOCUSED":
            state = "FOCUSED"
            available = True
            focused = True
            reason = "REQUEST_DOCKING_FOCUSED"
        elif visual["state"] == "VISIBLE":
            state = "AVAILABLE"
            available = True
            focused = False
            reason = "REQUEST_DOCKING_VISIBLE"
        else:
            reason = "REQUEST_TEXT_CONFIRMED_FOCUS_UNKNOWN"

    return {
        "schemaVersion": 1,
        "requestDocking": {"state": state, "available": available, "focused": focused},
        "source": {
            "activeTab": contacts["activeTab"],
            "searchRegion": raw["evidence"]["referenceRegion"],
            "ocrTiming": raw["timing"],
            "regionCount": len(regions),
            "anchor": {
                "text": anchor["region"]["text"],
                "normalizedText": anchor["normalizedText"],
                "referencePoints": anchor["region"]["referencePoints"],
                "bounds": anchor["bounds"],
                "similarity": anchor["similarity"],
                "margin": anchor_margin,
            },
            "text": None if best == None else best["region"]["text"],
            "normalizedText": None if best == None else best["normalizedText"],
            "referencePoints": None if best == None else best["region"]["referencePoints"],
            "leftContextRegion": None if best == None else best["region"]["leftContext"]["referenceRegion"],
            "visual": visual,
        },
        "decision": {
            "accepted": accepted,
            "candidate": None if best == None else best["label"],
            "candidateScore": 0.0 if best == None else best["score"],
            "candidateSimilarity": 0.0 if best == None else best["similarity"],
            "margin": margin,
            "meaningfulRegionCount": meaningful_count,
            "minimumDetectionConfidence": MIN_DETECTION_CONFIDENCE,
            "minimumRecognitionConfidence": MIN_RECOGNITION_CONFIDENCE,
            "minimumTextSimilarity": MIN_TEXT_SIMILARITY,
            "minimumTextMargin": MIN_TEXT_MARGIN,
            "minimumAnchorSimilarity": MIN_ANCHOR_SIMILARITY,
            "minimumAnchorMargin": MIN_ANCHOR_MARGIN,
            "actionOffsetBounds": {
                "minimumX": MIN_ACTION_HORIZONTAL_OFFSET,
                "maximumX": MAX_ACTION_HORIZONTAL_OFFSET,
                "minimumY": MIN_ACTION_VERTICAL_OFFSET,
                "maximumY": MAX_ACTION_VERTICAL_OFFSET,
            },
            "focusedBrightMinimum": FOCUSED_BRIGHT_MINIMUM,
            "visibleDarkMinimum": VISIBLE_DARK_MINIMUM,
            "denialNotificationDetected": denial_accepted,
            "denialNotificationOverridden": denial_overridden,
            "reason": reason,
        },
    }
