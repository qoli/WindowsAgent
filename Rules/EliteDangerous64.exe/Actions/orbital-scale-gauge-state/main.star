ROI_X = 1120
ROI_Y = 390
WIDTH = 145
HEIGHT = 330
MIN_SPINE_PIXELS = 120
MIN_SPINE_SPAN = 200
MIN_TICK_WIDTH = 14
MIN_TICK_PIXELS = 8
MIN_TICKS = 3
MIN_TICK_SPREAD = 120
CONFIDENCE_THRESHOLD = 0.75

def orange(red, green, blue):
    maximum = max(red, green, blue)
    minimum = min(red, green, blue)
    chroma = maximum - minimum
    if maximum < 80 or maximum != red or green < blue or chroma == 0:
        return False
    if chroma * 100 < maximum * 50:
        return False
    hue_times_six = 360 * (green - blue)
    return hue_times_six >= 5 * 6 * chroma and hue_times_six <= 45 * 6 * chroma

def channels(pixel):
    return pixel // 65536, (pixel // 256) % 256, pixel % 256

def main(ctx):
    sample = observer.screen.read_region(x=ROI_X, y=ROI_Y, w=WIDTH, h=HEIGHT, sampling="reference")
    pixels = sample["image"]["pixels"]
    if len(pixels) != WIDTH * HEIGHT:
        return job.fail(code="ORBITAL_SCALE_EVIDENCE_INVALID", message="orbital scale ROI pixel count is incomplete")

    mask = []
    orange_count = 0
    for pixel in pixels:
        red, green, blue = channels(pixel)
        matched = orange(red, green, blue)
        mask.append(matched)
        if matched:
            orange_count += 1

    spine_x = 0
    spine_pixels = 0
    spine_top = None
    spine_bottom = None
    for x in range(WIDTH):
        count = 0
        first = None
        last = None
        for y in range(HEIGHT):
            if not mask[y * WIDTH + x]:
                continue
            count += 1
            if first == None:
                first = y
            last = y
        if count > spine_pixels:
            spine_x = x
            spine_pixels = count
            spine_top = first
            spine_bottom = last

    spine_span = spine_bottom - spine_top + 1 if spine_top != None else 0
    scan_left = max(0, spine_x - 5)
    scan_right = min(WIDTH - 1, spine_x + 45)
    tick_rows = []
    for y in range(HEIGHT):
        first = None
        last = None
        count = 0
        for x in range(scan_left, scan_right + 1):
            if not mask[y * WIDTH + x]:
                continue
            count += 1
            if first == None:
                first = x
            last = x
        if first != None and count >= MIN_TICK_PIXELS and last - first + 1 >= MIN_TICK_WIDTH:
            tick_rows.append(y)

    # HDR bloom and anti-aliasing can split one horizontal mark by a few rows.
    # Merge rows separated by at most three blank rows, then retain only the
    # compact marks. The coordinate readout below the scale is deliberately
    # rejected because it forms a much taller component.
    tick_components = []
    current = None
    for y in tick_rows:
        if current == None or y > current["bottom"] + 4:
            if current != None:
                tick_components.append(current)
            current = {"top": y, "bottom": y}
        else:
            current["bottom"] = y
    if current != None:
        tick_components.append(current)

    ticks = []
    for component in tick_components:
        height = component["bottom"] - component["top"] + 1
        if height <= 16:
            ticks.append((component["top"] + component["bottom"]) // 2)

    tick_spread = ticks[-1] - ticks[0] if len(ticks) > 1 else 0
    spine_pixel_score = min(1.0, float(spine_pixels) / 180.0)
    spine_span_score = min(1.0, float(spine_span) / 220.0)
    tick_score = min(1.0, float(len(ticks)) / float(MIN_TICKS))
    spread_score = min(1.0, float(tick_spread) / float(MIN_TICK_SPREAD))
    confidence = spine_pixel_score * 0.45 + spine_span_score * 0.25 + tick_score * 0.25 + spread_score * 0.05

    geometry_confirmed = spine_pixels >= MIN_SPINE_PIXELS and spine_span >= MIN_SPINE_SPAN and len(ticks) >= MIN_TICKS and tick_spread >= MIN_TICK_SPREAD
    state = "DETECTED" if geometry_confirmed and confidence >= CONFIDENCE_THRESHOLD else "ABSENT"
    reason = "ORBITAL_VERTICAL_SCALE_GEOMETRY_CONFIRMED" if state == "DETECTED" else "ORBITAL_VERTICAL_SCALE_GEOMETRY_NOT_CONFIRMED"
    return {
        "schemaVersion": 2,
        "gauge": {
            "state": state,
            "confidence": math.round(confidence * 10000.0) / 10000.0,
            "threshold": CONFIDENCE_THRESHOLD,
            "reason": reason,
        },
        "evidence": {
            "capturedAt": sample["frame"]["capturedAt"],
            "referenceRegion": {"x": ROI_X, "y": ROI_Y, "w": WIDTH, "h": HEIGHT},
            "algorithm": "HSV_ORANGE_ORBITAL_VERTICAL_SCALE_GEOMETRY_V2",
            "orangePixelCount": orange_count,
            "spineX": ROI_X + spine_x,
            "spinePixelCount": spine_pixels,
            "spineSpanPixels": spine_span,
            "tickCount": len(ticks),
            "tickSpreadPixels": tick_spread,
        },
    }
