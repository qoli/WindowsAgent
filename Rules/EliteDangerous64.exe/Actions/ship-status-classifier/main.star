MIN_LABEL_CONFIDENCE = 0.45
COLOR_FRACTION_DENOMINATOR = 500

TARGETS = [
    {"key": "massLock", "prefix": "MASS"},
    {"key": "landingGear", "prefix": "LANDING"},
    {"key": "cargoScoop", "prefix": "CARGO"},
]

def normalize_text(text):
    result = ""
    upper = text.upper()
    for index in range(len(upper)):
        character = upper[index]
        if character in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789":
            result += character
    return result

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

def prefix_similarity(text, prefix):
    if len(text) == 0:
        return 0.0
    candidate = text
    if len(candidate) > len(prefix):
        candidate = candidate[:len(prefix)]
    maximum = len(prefix)
    if len(candidate) > maximum:
        maximum = len(candidate)
    return 1.0 - float(edit_distance(candidate, prefix)) / float(maximum)

def pixel_channels(pixel):
    return pixel // 65536, (pixel // 256) % 256, pixel % 256

def is_orange(red, green, blue):
    return red >= 165 and green >= 45 and green <= 215 and blue <= 125 and red >= green + 30

def is_cyan(red, green, blue):
    return green >= 130 and blue >= 120 and red <= 190 and green >= red + 15 and blue >= red

def select_anchor(regions, prefix):
    selected = None
    for region in regions:
        normalized = normalize_text(region["text"])
        similarity = prefix_similarity(normalized, prefix)
        score = region["recognitionConfidence"] * similarity
        candidate = {
            "region": region,
            "normalizedText": normalized,
            "prefixSimilarity": similarity,
            "confidence": score,
            "accepted": region["detectionConfidence"] >= 0.45 and score >= MIN_LABEL_CONFIDENCE,
        }
        if selected == None or candidate["confidence"] > selected["confidence"]:
            selected = candidate
    return selected

def unknown_status(reason, anchor):
    return {
        "state": "UNKNOWN",
        "on": None,
        "color": None,
        "referenceX": None,
        "referenceY": None,
        "evidence": {
            "reason": reason,
            "text": None if anchor == None else anchor["region"]["text"],
            "normalizedText": None if anchor == None else anchor["normalizedText"],
            "detectionConfidence": 0.0 if anchor == None else anchor["region"]["detectionConfidence"],
            "recognitionConfidence": 0.0 if anchor == None else anchor["region"]["recognitionConfidence"],
            "prefixSimilarity": 0.0 if anchor == None else anchor["prefixSimilarity"],
            "labelConfidence": 0.0 if anchor == None else anchor["confidence"],
            "labelThreshold": MIN_LABEL_CONFIDENCE,
            "cyanPixelCount": 0,
            "orangePixelCount": 0,
            "colorPixelThreshold": 0,
        },
    }

def classify_indicator(anchor):
    if anchor == None or not anchor["accepted"]:
        return unknown_status("LABEL_NOT_CONFIRMED", anchor)
    context = anchor["region"]["leftContext"]
    width = context["w"]
    height = context["h"]
    pixels = context["pixels"]
    if width <= 0 or height <= 0 or len(pixels) != width * height:
        return unknown_status("LEFT_CONTEXT_INVALID", anchor)

    start_x = width // 4
    end_x = width - 1
    start_y = height // 10
    end_y = height - height // 10
    area = (end_x - start_x) * (end_y - start_y)
    threshold = area // COLOR_FRACTION_DENOMINATOR
    if threshold < 6:
        threshold = 6
    cyan = 0
    orange = 0
    cyan_x = 0
    cyan_y = 0
    orange_x = 0
    orange_y = 0
    for y in range(start_y, end_y):
        for x in range(start_x, end_x):
            red, green, blue = pixel_channels(pixels[y * width + x])
            if is_cyan(red, green, blue):
                cyan += 1
                cyan_x += x
                cyan_y += y
            if is_orange(red, green, blue):
                orange += 1
                orange_x += x
                orange_y += y

    state = "UNKNOWN"
    color = None
    on = None
    selected_count = 0
    selected_x = 0
    selected_y = 0
    reason = "INDICATOR_COLOR_AMBIGUOUS"
    if cyan >= threshold:
        state = "ON"
        color = "cyan"
        on = True
        selected_count = cyan
        selected_x = cyan_x
        selected_y = cyan_y
        reason = "CYAN_INDICATOR_CONFIRMED"
    elif orange >= threshold:
        state = "OFF"
        color = "orange"
        on = False
        selected_count = orange
        selected_x = orange_x
        selected_y = orange_y
        reason = "ORANGE_INDICATOR_CONFIRMED"

    reference_x = None
    reference_y = None
    if selected_count > 0:
        reference = context["referenceRegion"]
        reference_x = reference["x"] + (float(selected_x) / float(selected_count) + 0.5) * reference["w"] / float(width)
        reference_y = reference["y"] + (float(selected_y) / float(selected_count) + 0.5) * reference["h"] / float(height)
    return {
        "state": state,
        "on": on,
        "color": color,
        "referenceX": reference_x,
        "referenceY": reference_y,
        "evidence": {
            "reason": reason,
            "text": anchor["region"]["text"],
            "normalizedText": anchor["normalizedText"],
            "detectionConfidence": anchor["region"]["detectionConfidence"],
            "recognitionConfidence": anchor["region"]["recognitionConfidence"],
            "prefixSimilarity": anchor["prefixSimilarity"],
            "labelConfidence": anchor["confidence"],
            "labelThreshold": MIN_LABEL_CONFIDENCE,
            "cyanPixelCount": cyan,
            "orangePixelCount": orange,
            "colorPixelThreshold": threshold,
        },
    }

def main(ctx):
    raw = ctx.inputs
    statuses = {}
    for target in TARGETS:
        statuses[target["key"]] = classify_indicator(select_anchor(raw["regions"], target["prefix"]))
    return {
        "schemaVersion": 4,
        "profile": {
            "width": raw["evidence"]["frame"]["width"],
            "height": raw["evidence"]["frame"]["height"],
            "capturedAt": raw["evidence"]["capturedAt"],
        },
        "coordinateSpace": raw["evidence"]["coordinateSpace"],
        "region": raw["evidence"]["referenceRegion"],
        "physicalRegion": raw["evidence"]["physicalRegion"],
        "shipStatus": statuses,
        "evidence": {
            "ocrRegionCount": len(raw["regions"]),
            "models": raw["models"],
            "timing": raw["timing"],
        },
    }
