ROI_X = 640
ROI_Y = 235
WIDTH = 490
HEIGHT = 133
SCAN_TOP = 98
SCAN_BOTTOM = 132
MIN_TICK_HEIGHT = 6
MIN_TICKS = 6
MIN_SPACING = 24
MAX_SPACING = 37
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

    orange_count = 0
    upper_count = 0
    left_anchor_count = 0
    right_anchor_count = 0
    column_runs = []
    for x in range(WIDTH):
        first = None
        last = None
        count = 0
        for y in range(HEIGHT):
            red, green, blue = channels(pixels[y * WIDTH + x])
            if not orange(red, green, blue):
                continue
            orange_count += 1
            if y >= 55 and y < SCAN_TOP:
                upper_count += 1
                if x < WIDTH // 2 - 35:
                    left_anchor_count += 1
                elif x > WIDTH // 2 + 35:
                    right_anchor_count += 1
            if y >= SCAN_TOP and y <= SCAN_BOTTOM:
                count += 1
                if first == None:
                    first = y
                last = y
        if first != None and count >= 4 and last - first + 1 >= MIN_TICK_HEIGHT:
            column_runs.append({"x": x, "top": first, "bottom": last, "height": last - first + 1})

    components = []
    current = None
    for run in column_runs:
        if current == None or run["x"] > current["right"] + 1:
            if current != None:
                components.append(current)
            current = {"left": run["x"], "right": run["x"], "top": run["top"], "bottom": run["bottom"]}
        else:
            current["right"] = run["x"]
            current["top"] = min(current["top"], run["top"])
            current["bottom"] = max(current["bottom"], run["bottom"])
    if current != None:
        components.append(current)

    ticks = []
    for component in components:
        width = component["right"] - component["left"] + 1
        height = component["bottom"] - component["top"] + 1
        # HDR bloom widens the reviewed 2-4px glyph core to as much as 14
        # reference pixels. Width remains bounded so number strokes do not
        # become scale ticks.
        if width <= 18 and height >= MIN_TICK_HEIGHT and component["bottom"] >= 116:
            ticks.append({"center": (component["left"] + component["right"]) // 2, "top": component["top"], "bottom": component["bottom"], "height": height})

    spacing_matches = 0
    collinear_matches = 0
    spacing_sum = 0
    for index in range(1, len(ticks)):
        gap = ticks[index]["center"] - ticks[index - 1]["center"]
        if gap >= MIN_SPACING and gap <= MAX_SPACING:
            spacing_matches += 1
            spacing_sum += gap
            if abs(ticks[index]["bottom"] - ticks[index - 1]["bottom"]) <= 3:
                collinear_matches += 1

    minor_height_sum = 0
    minor_height_count = 0
    maximum_height = 0
    major_near_center = False
    for tick in ticks:
        maximum_height = max(maximum_height, tick["height"])
        if tick["height"] <= 22:
            minor_height_sum += tick["height"]
            minor_height_count += 1
    average_minor_height = float(minor_height_sum) / float(minor_height_count) if minor_height_count > 0 else 0.0
    for tick in ticks:
        if abs(tick["center"] - WIDTH // 2) <= 55 and average_minor_height > 0 and float(tick["height"]) >= average_minor_height * 1.55:
            major_near_center = True

    tick_score = min(1.0, float(len(ticks)) / float(MIN_TICKS))
    spacing_score = min(1.0, float(spacing_matches) / 5.0)
    collinear_score = min(1.0, float(collinear_matches) / 4.0)
    geometry_score = tick_score * 0.36 + spacing_score * 0.34 + collinear_score * 0.18 + (0.12 if major_near_center else 0.0)
    anchor_score = min(1.0, float(upper_count) / 180.0) * 0.7
    if left_anchor_count >= 20 or right_anchor_count >= 20:
        anchor_score += 0.3
    density_score = min(1.0, float(orange_count) / 800.0)
    confidence = geometry_score * 0.62 + anchor_score * 0.30 + density_score * 0.08

    state = "DETECTED" if confidence >= CONFIDENCE_THRESHOLD else "ABSENT"
    reason = "ORBITAL_HEADING_SCALE_GEOMETRY_CONFIRMED" if state == "DETECTED" else "ORBITAL_HEADING_SCALE_GEOMETRY_NOT_CONFIRMED"
    return {
        "schemaVersion": 1,
        "gauge": {
            "state": state,
            "confidence": math.round(confidence * 10000.0) / 10000.0,
            "threshold": CONFIDENCE_THRESHOLD,
            "reason": reason,
        },
        "evidence": {
            "capturedAt": sample["frame"]["capturedAt"],
            "referenceRegion": {"x": ROI_X, "y": ROI_Y, "w": WIDTH, "h": HEIGHT},
            "algorithm": "HSV_ORANGE_VERTICAL_SCALE_GEOMETRY_V1",
            "orangePixelCount": orange_count,
            "tickCount": len(ticks),
            "spacingMatchCount": spacing_matches,
            "collinearMatchCount": collinear_matches,
            "averageSpacingPixels": math.round(float(spacing_sum) / float(spacing_matches) * 100.0) / 100.0 if spacing_matches > 0 else None,
            "averageMinorTickHeightPixels": math.round(average_minor_height * 100.0) / 100.0 if minor_height_count > 0 else None,
            "maximumTickHeightPixels": maximum_height,
            "majorTickNearCenter": major_near_center,
            "upperOrangePixelCount": upper_count,
            "leftAnchorPixelCount": left_anchor_count,
            "rightAnchorPixelCount": right_anchor_count,
        },
    }
