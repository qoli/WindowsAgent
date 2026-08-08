MIN_DETECTION_CONFIDENCE = 0.45
MIN_RECOGNITION_CONFIDENCE = 0.50
MIN_TEXT_SIMILARITY = 0.70
MIN_TEXT_MARGIN = 0.12
AMBIGUOUS_TEXT_SIMILARITY = 0.45
FOCUSED_BRIGHT_MINIMUM = 0.10
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

def select_candidate(regions, label, expected):
    best = None
    for region in regions:
        candidate = candidate_for(region, label, expected)
        if best == None or candidate["score"] > best["score"]:
            best = candidate
    return best

def select_candidates(regions):
    request = select_candidate(regions, "REQUEST", "REQUESTDOCKING")
    cancel = select_candidate(regions, "CANCEL", "CANCELDOCKING")
    if request == None or (cancel != None and cancel["score"] > request["score"]):
        return cancel, request
    return request, cancel

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
        "source": {"contactsTab": contacts["contactsTab"], "regionCount": None, "text": None, "normalizedText": None, "referencePoints": None, "leftContextRegion": None, "visual": None},
        "decision": {"accepted": False, "candidate": None, "candidateScore": 0.0, "candidateSimilarity": 0.0, "margin": 0.0, "reason": reason},
    }

def main(ctx):
    contacts = ctx.inputs["contacts"]
    if contacts["contactsTab"]["state"] != "SELECTED":
        return unknown_result(contacts, "CONTACTS_TAB_NOT_SELECTED")
    raw = ctx.inputs["regions"]
    if raw == None:
        return unknown_result(contacts, "TEXT_REGIONS_MISSING")
    regions = raw["regions"]
    meaningful_count = meaningful_region_count(regions)
    best, runner_up = select_candidates(regions)
    runner_score = 0.0 if runner_up == None else runner_up["score"]
    margin = 0.0 if best == None else best["score"] - runner_score
    accepted = best != None and best["region"]["detectionConfidence"] >= MIN_DETECTION_CONFIDENCE and best["region"]["recognitionConfidence"] >= MIN_RECOGNITION_CONFIDENCE and best["similarity"] >= MIN_TEXT_SIMILARITY and margin >= MIN_TEXT_MARGIN

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
            reason = "CANCEL_DOCKING_CONFIRMED"
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
            "contactsTab": contacts["contactsTab"],
            "regionCount": len(regions),
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
            "focusedBrightMinimum": FOCUSED_BRIGHT_MINIMUM,
            "visibleDarkMinimum": VISIBLE_DARK_MINIMUM,
            "reason": reason,
        },
    }
