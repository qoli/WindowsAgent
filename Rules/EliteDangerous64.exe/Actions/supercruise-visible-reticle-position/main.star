REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
SCREEN_CENTER_X = 960
SCREEN_CENTER_Y = 540
ROI_SIZE = 140
ROI_HALF = 70
CANDIDATE_SPAN = 28
CANDIDATE_STEP = 4
PIXEL_STEP = 2
INNER_RADIUS_SQUARED = 34 * 34
OUTER_RADIUS_SQUARED = 58 * 58
MIN_RING_SCORE = 18
AMBIGUOUS_SCORE_MARGIN = 2
DISTINCT_CENTER_DISTANCE_SQUARED = 20 * 20

def is_orange(pixel):
    red = pixel // 65536
    green = (pixel // 256) % 256
    blue = pixel % 256
    return red >= 170 and green >= 55 and green <= 210 and blue <= 110 and red >= green + 35

def unknown(reason, best_score=0, second_score=0):
    return {
        "schemaVersion": 1,
        "target": {"state": "UNKNOWN", "referenceX": None, "referenceY": None, "offsetX": None, "offsetY": None, "centerDistancePixels": None, "reason": reason, "bestScore": best_score, "secondScore": second_score},
        "evidence": {},
    }

def main(ctx):
    hint_x = ctx.inputs["hintX"]
    hint_y = ctx.inputs["hintY"]
    roi_x = hint_x - ROI_HALF
    roi_y = hint_y - ROI_HALF
    sample = observer.screen.read_region(x=roi_x, y=roi_y, w=ROI_SIZE, h=ROI_SIZE, sampling="reference")
    image = sample["image"]
    if sample["sampling"] != "reference" or sample["coordinateSpace"]["width"] != REFERENCE_WIDTH or sample["coordinateSpace"]["height"] != REFERENCE_HEIGHT or image["encoding"] != "rgb24-packed" or len(image["pixels"]) != ROI_SIZE * ROI_SIZE:
        return job.fail(code="SUPERCRUISE_RETICLE_EVIDENCE_INVALID", message="local reticle screen evidence is incomplete")
    orange_points = []
    for pixel_index in range(len(image["pixels"])):
        if is_orange(image["pixels"][pixel_index]):
            orange_points.append([pixel_index % ROI_SIZE, pixel_index // ROI_SIZE])
    best_score = -1
    second_score = -1
    second_x = None
    second_y = None
    best_x = None
    best_y = None
    for candidate_y in range(ROI_HALF - CANDIDATE_SPAN, ROI_HALF + CANDIDATE_SPAN + 1, CANDIDATE_STEP):
        for candidate_x in range(ROI_HALF - CANDIDATE_SPAN, ROI_HALF + CANDIDATE_SPAN + 1, CANDIDATE_STEP):
            score = 0
            for point in orange_points:
                if point[0] % PIXEL_STEP != 0 or point[1] % PIXEL_STEP != 0:
                    continue
                dx = point[0] - candidate_x
                dy = point[1] - candidate_y
                radius_squared = dx * dx + dy * dy
                if radius_squared >= INNER_RADIUS_SQUARED and radius_squared <= OUTER_RADIUS_SQUARED:
                    score += 1
            if score > best_score:
                second_score = best_score
                second_x = best_x
                second_y = best_y
                best_score = score
                best_x = candidate_x
                best_y = candidate_y
            elif score > second_score:
                second_score = score
                second_x = candidate_x
                second_y = candidate_y
    if best_score < MIN_RING_SCORE:
        return unknown("ORANGE_RETICLE_SCORE_LOW", best_score, max(0, second_score))
    distinct_second = second_x != None and (second_x - best_x) * (second_x - best_x) + (second_y - best_y) * (second_y - best_y) >= DISTINCT_CENTER_DISTANCE_SQUARED
    if distinct_second and best_score - second_score <= AMBIGUOUS_SCORE_MARGIN:
        return unknown("ORANGE_RETICLE_CENTER_AMBIGUOUS", best_score, second_score)
    reference_x = roi_x + best_x
    reference_y = roi_y + best_y
    offset_x = reference_x - SCREEN_CENTER_X
    offset_y = reference_y - SCREEN_CENTER_Y
    return {
        "schemaVersion": 1,
        "target": {"state": "DETECTED", "referenceX": reference_x, "referenceY": reference_y, "offsetX": offset_x, "offsetY": offset_y, "centerDistancePixels": math.hypot(offset_x, offset_y), "reason": "ORANGE_RETICLE_ANNULUS_CENTER_CONFIRMED", "bestScore": best_score, "secondScore": max(0, second_score)},
        "evidence": {"region": sample["region"], "physicalRegion": sample["physicalRegion"], "capturedAt": sample["frame"]["capturedAt"], "orangePixelCount": len(orange_points)},
    }
