# Rule-internal pure classifier. The public flight-status Action owns capture.
MIN_STATUS_CONFIDENCE = 0.30
MIN_STATUS_MARGIN = 0.10
MIN_TEXT_SIMILARITY = 0.60

STATUS_RULES = [
    {
        "state": "SUPERCRUISE_ASSIST_ACTIVE",
        "aliases": ["SUPERCRUISE ASSIST ACTIVE"],
    },
    {
        "state": "SUPERCRUISE_ASSIST_LINE_OF_SIGHT_REQUIRED",
        "aliases": ["MOVE TO OBTAIN LINE OF SIGHT TO TARGET"],
    },
    {
        "state": "SUPERCRUISE",
        "aliases": ["SUPERCRUISE"],
    },
    {
        "state": "AUTO_LAUNCH",
        "aliases": ["AUTO LAUNCH IN PROGRESS"],
    },
    {
        "state": "WAITING_IN_QUEUE",
        "aliases": ["WAITING IN QUEUE"],
    },
    {
        "state": "SLOW_DOWN_FOR_AUTO_DOCK",
        "aliases": ["SLOW DOWN FOR AUTO DOCK"],
    },
    {
        "state": "FSD_CHARGING",
        "aliases": ["PRESS TO ABORT", "CHARGING", "CHARGING PRESS TO ABORT"],
    },
    {
        "state": "FSD_THROTTLE_UP_REQUIRED",
        "aliases": ["THROTTLE UP TO ENGAGE"],
    },
    {
        "state": "FSD_ALIGNMENT_REQUIRED",
        "aliases": ["ALIGN WITH TARGET DESTINATION"],
    },
    {
        "state": "FSD_ESCAPE_VECTOR_REQUIRED",
        "aliases": ["ALIGN WITH ESCAPE VECTOR"],
    },
    {
        "state": "SAFE_DISENGAGE_READY",
        "aliases": ["SAFE DISENGAGE READY"],
    },
    {
        "state": "AUTO_DOCK",
        "aliases": ["AUTO DOCK IN PROGRESS"],
    },
]

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

def text_similarity(left, right):
    maximum = len(left)
    if len(right) > maximum:
        maximum = len(right)
    if maximum == 0:
        return 0.0
    return 1.0 - float(edit_distance(left, right)) / float(maximum)

def best_alias(normalized_text, aliases):
    selected_alias = None
    selected_similarity = -1.0
    for alias in aliases:
        similarity = text_similarity(normalized_text, normalize_text(alias))
        if similarity > selected_similarity:
            selected_alias = alias
            selected_similarity = similarity
    return selected_alias, selected_similarity

def classify(normalized_text, ocr_confidence):
    best = None
    runner_up = None
    for rule in STATUS_RULES:
        alias, similarity = best_alias(normalized_text, rule["aliases"])
        candidate = {
            "state": rule["state"],
            "alias": alias,
            "textSimilarity": similarity,
            "confidence": ocr_confidence * similarity,
        }
        if best == None or candidate["confidence"] > best["confidence"]:
            runner_up = best
            best = candidate
        elif runner_up == None or candidate["confidence"] > runner_up["confidence"]:
            runner_up = candidate
    return best, runner_up

def empty_decision():
    return {
        "accepted": False,
        "confidence": 0.0,
        "threshold": MIN_STATUS_CONFIDENCE,
        "margin": 0.0,
        "marginThreshold": MIN_STATUS_MARGIN,
        "similarityThreshold": MIN_TEXT_SIMILARITY,
        "candidateState": None,
        "candidateAlias": None,
        "textSimilarity": 0.0,
        "runnerUpState": None,
        "runnerUpConfidence": 0.0,
    }

def main(ctx):
    raw = ctx.inputs
    normalized_text = normalize_text(raw["text"])
    decision = empty_decision()
    state = "UNKNOWN"
    if len(normalized_text) > 0 and raw["confidence"] > 0.0:
        best, runner_up = classify(normalized_text, raw["confidence"])
        margin = best["confidence"] - runner_up["confidence"]
        accepted = best["confidence"] >= MIN_STATUS_CONFIDENCE and best["textSimilarity"] >= MIN_TEXT_SIMILARITY and margin >= MIN_STATUS_MARGIN
        if accepted:
            state = best["state"]
        decision = {
            "accepted": accepted,
            "confidence": best["confidence"],
            "threshold": MIN_STATUS_CONFIDENCE,
            "margin": margin,
            "marginThreshold": MIN_STATUS_MARGIN,
            "similarityThreshold": MIN_TEXT_SIMILARITY,
            "candidateState": best["state"],
            "candidateAlias": best["alias"],
            "textSimilarity": best["textSimilarity"],
            "runnerUpState": runner_up["state"],
            "runnerUpConfidence": runner_up["confidence"],
        }
    return {
        "schemaVersion": 1,
        "flightStatus": {
            "state": state,
            "known": state != "UNKNOWN",
        },
        "source": {
            "text": raw["text"],
            "normalizedText": normalized_text,
            "ocrConfidence": raw["confidence"],
        },
        "decision": decision,
    }
