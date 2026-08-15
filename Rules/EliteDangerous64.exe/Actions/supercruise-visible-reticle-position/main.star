REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
SCREEN_CENTER_X = 960
SCREEN_CENTER_Y = 540
ROI_SIZE = 140
ROI_HALF = 70
CANDIDATE_SPAN = 28
CANDIDATE_STEP = 4
REFINEMENT_RADIUS = 4
INNER_RADIUS_SQUARED = 34 * 34
OUTER_RADIUS_SQUARED = 58 * 58
SEARCH_INNER_RADIUS_SQUARED = 40 * 40
SEARCH_OUTER_RADIUS_SQUARED = 52 * 52
INNER_NOISE_MIN_RADIUS_SQUARED = 28 * 28
INNER_NOISE_MAX_RADIUS_SQUARED = 38 * 38
OUTER_NOISE_MIN_RADIUS_SQUARED = 54 * 54
OUTER_NOISE_MAX_RADIUS_SQUARED = 64 * 64
MIN_RING_SCORE = 18
MIN_DASHED_OCCUPIED_ANGULAR_BINS = 18
MIN_SOLID_OCCUPIED_ANGULAR_BINS = 40
AMBIGUOUS_QUALITY_MARGIN = 10
DISTINCT_CENTER_DISTANCE_SQUARED = 20 * 20
PLANE_DISAGREEMENT_DISTANCE_SQUARED = 20 * 20
PLANE_DISAGREEMENT_BIN_MARGIN = 2
ANGULAR_BINS = 72
MIN_DASHED_ANGULAR_RUNS = 5
# The selected-target focus frame is intentionally open on the label side. The
# eighteen bins centred on +X are the reviewed one-quarter label sector; the
# remaining fifty-four bins are the structural three-quarter arc.
LABEL_SECTOR_START = 27
LABEL_SECTOR_END = 44
STRUCTURAL_ARC_BINS = 54
LABEL_SECTOR_BINS = 18
MIN_OTSU_NONZERO_PIXELS = 24
MAX_EVIDENCE_PLANE_PIXELS = 10000

def channels(pixel):
    return [pixel // 65536, (pixel // 256) % 256, pixel % 256]

def strict_rgb_orange(red, green, blue):
    return red >= 170 and green >= 55 and green <= 210 and blue <= 110 and red >= green + 35

def opponent_score(red, green, blue):
    orange = min(red, 2 * green) - blue
    if orange <= 0:
        return 0
    return int(255.0 * float(orange) / float(red + green + blue + 1))

def hsv_orange_score(red, green, blue):
    maximum = max(red, green, blue)
    minimum = min(red, green, blue)
    delta = maximum - minimum
    # The ED HUD hue lies around OpenCV HSV H=16 (32 degrees). Limiting the
    # score to red-dominant pixels with green at least blue rejects magenta
    # bodies without imposing the old FP32-era absolute-brightness gate.
    if maximum == 0 or delta == 0 or red != maximum or green < blue:
        return 0
    hue = 30.0 * float(green - blue) / float(delta)
    affinity = 1.0 - abs(hue - 16.0) / 18.0
    if affinity <= 0:
        return 0
    saturation = float(delta) / float(maximum)
    value_weight = math.sqrt(float(maximum) / 255.0)
    return int(255.0 * affinity * saturation * value_weight)

def otsu_threshold(histogram):
    # Zero is not orange evidence. Including the black/background population in
    # Otsu makes the result depend on how much empty HUD space happens to lie in
    # the hint window, so threshold only the retained non-zero scores.
    nonzero_count = 0
    nonzero_sum = 0
    minimum = None
    maximum = None
    for score in range(1, 256):
        count = histogram[score]
        if count == 0:
            continue
        nonzero_count += count
        nonzero_sum += score * count
        if minimum == None:
            minimum = score
        maximum = score
    if nonzero_count < MIN_OTSU_NONZERO_PIXELS or minimum == maximum:
        return {
            "method": "OTSU", "threshold": 0,
            "nonzeroCount": nonzero_count,
            "nonzeroRatioPermille": nonzero_count * 1000 // (ROI_SIZE * ROI_SIZE),
            "degenerate": True,
        }

    background_count = 0
    background_sum = 0
    best_variance = -1.0
    best_threshold = 0
    for score in range(1, 256):
        background_count += histogram[score]
        background_sum += score * histogram[score]
        foreground_count = nonzero_count - background_count
        if background_count == 0 or foreground_count == 0:
            continue
        background_mean = float(background_sum) / float(background_count)
        foreground_mean = float(nonzero_sum - background_sum) / float(foreground_count)
        difference = background_mean - foreground_mean
        variance = float(background_count * foreground_count) * difference * difference
        if variance > best_variance:
            best_variance = variance
            best_threshold = score + 1
    if best_threshold <= 1 or best_threshold > 255:
        return {
            "method": "OTSU", "threshold": 0,
            "nonzeroCount": nonzero_count,
            "nonzeroRatioPermille": nonzero_count * 1000 // (ROI_SIZE * ROI_SIZE),
            "degenerate": True,
        }
    return {
        "method": "OTSU", "threshold": best_threshold,
        "nonzeroCount": nonzero_count,
        "nonzeroRatioPermille": nonzero_count * 1000 // (ROI_SIZE * ROI_SIZE),
        "degenerate": False,
    }

def points_at_or_above(scores, threshold):
    points = []
    for index in range(len(scores)):
        if scores[index] > 0 and scores[index] >= threshold:
            points.append([index % ROI_SIZE, index // ROI_SIZE])
    return points

def candidate_score(points, candidate_x, candidate_y):
    ring_score = 0
    clutter_score = 0
    for point in points:
        dx = point[0] - candidate_x
        dy = point[1] - candidate_y
        radius_squared = dx * dx + dy * dy
        if radius_squared >= SEARCH_INNER_RADIUS_SQUARED and radius_squared <= SEARCH_OUTER_RADIUS_SQUARED:
            ring_score += 1
        elif (radius_squared >= INNER_NOISE_MIN_RADIUS_SQUARED and radius_squared <= INNER_NOISE_MAX_RADIUS_SQUARED) or (radius_squared >= OUTER_NOISE_MIN_RADIUS_SQUARED and radius_squared <= OUTER_NOISE_MAX_RADIUS_SQUARED):
            clutter_score += 1
    hint_distance = abs(candidate_x - ROI_HALF) + abs(candidate_y - ROI_HALF)
    quality = ring_score * 5 - clutter_score * 7 - hint_distance
    return [quality, ring_score, clutter_score]

def topology(points, center_x, center_y):
    angular_bins = []
    for _ in range(ANGULAR_BINS):
        angular_bins.append(False)
    for point in points:
        dx = point[0] - center_x
        dy = point[1] - center_y
        radius_squared = dx * dx + dy * dy
        if radius_squared < INNER_RADIUS_SQUARED or radius_squared > OUTER_RADIUS_SQUARED:
            continue
        angle = math.atan2(dy, dx) + math.pi
        angular_bin = int(angle * float(ANGULAR_BINS) / (2.0 * math.pi)) % ANGULAR_BINS
        angular_bins[angular_bin] = True
    occupied = 0
    transitions = 0
    structural_occupied = 0
    label_sector_occupied = 0
    for index in range(ANGULAR_BINS):
        if angular_bins[index]:
            occupied += 1
            if index >= LABEL_SECTOR_START and index <= LABEL_SECTOR_END:
                label_sector_occupied += 1
            else:
                structural_occupied += 1
        if angular_bins[index] != angular_bins[(index - 1) % ANGULAR_BINS]:
            transitions += 1
    return [occupied, transitions, transitions // 2, structural_occupied, label_sector_occupied]

def clamp_permille(value):
    if value < 0:
        return 0
    if value > 1000:
        return 1000
    return value

def shape_confidence(best, second_quality, shape):
    structural_coverage = clamp_permille(shape[3] * 1000 // STRUCTURAL_ARC_BINS)
    label_gap_clarity = clamp_permille((LABEL_SECTOR_BINS - shape[4]) * 1000 // LABEL_SECTOR_BINS)
    radial_contrast = clamp_permille(best["ringScore"] * 1000 // (best["ringScore"] + best["clutterScore"] + 1))
    centre_margin = best["quality"] if second_quality <= 0 else best["quality"] - second_quality
    centre_uniqueness = clamp_permille(centre_margin * 25)
    # Structural arc dominates. A bright label may occupy some of the open
    # sector, so gap clarity is corroborative and cannot veto an otherwise
    # strong three-quarter arc by itself.
    confidence = (structural_coverage * 55 + radial_contrast * 20 + centre_uniqueness * 15 + label_gap_clarity * 10) // 100
    return [confidence, structural_coverage, label_gap_clarity, radial_contrast, centre_uniqueness]

def evaluate_plane(name, points, threshold_evidence):
    threshold = threshold_evidence["threshold"]
    base = {
        "name": name, "state": "REJECTED", "reason": "RING_SCORE_LOW",
        "thresholdMethod": threshold_evidence["method"],
        "threshold": threshold,
        "nonzeroCount": threshold_evidence["nonzeroCount"],
        "nonzeroRatioPermille": threshold_evidence["nonzeroRatioPermille"],
        "thresholdDegenerate": threshold_evidence["degenerate"],
        "pixelCount": len(points), "x": ROI_HALF, "y": ROI_HALF,
        "quality": 0, "ringScore": 0, "secondQuality": 0, "secondRingScore": 0,
        "clutterScore": 0, "centerMargin": 0,
        "occupiedAngularBins": 0, "angularTransitions": 0,
        "angularRuns": 0, "structuralArcOccupiedBins": 0,
        "labelSectorOccupiedBins": 0, "structuralCoveragePermille": 0,
        "labelGapClarityPermille": 0, "radialContrastPermille": 0,
        "centerUniquenessPermille": 0, "shapeConfidencePermille": 0,
        "presentation": None,
    }
    if threshold_evidence["degenerate"]:
        base["reason"] = "HISTOGRAM_DEGENERATE"
        return base
    if len(points) > MAX_EVIDENCE_PLANE_PIXELS:
        base["reason"] = "PIXEL_DENSITY_HIGH"
        return base
    candidates = []
    coarse_best = None
    for candidate_y in range(ROI_HALF - CANDIDATE_SPAN, ROI_HALF + CANDIDATE_SPAN + 1, CANDIDATE_STEP):
        for candidate_x in range(ROI_HALF - CANDIDATE_SPAN, ROI_HALF + CANDIDATE_SPAN + 1, CANDIDATE_STEP):
            score = candidate_score(points, candidate_x, candidate_y)
            candidate = {
                "x": candidate_x,
                "y": candidate_y,
                "quality": score[0],
                "ringScore": score[1],
                "clutterScore": score[2],
            }
            candidates.append(candidate)
            if coarse_best == None or candidate["quality"] > coarse_best["quality"]:
                coarse_best = candidate
    # The four-pixel grid bounds global search cost. A fixed one-pixel local
    # refinement removes the measured hint/grid phase flicker without changing
    # capture, ROI, evidence plane, or frame.
    best = None
    for candidate_y in range(coarse_best["y"] - REFINEMENT_RADIUS, coarse_best["y"] + REFINEMENT_RADIUS + 1):
        for candidate_x in range(coarse_best["x"] - REFINEMENT_RADIUS, coarse_best["x"] + REFINEMENT_RADIUS + 1):
            score = candidate_score(points, candidate_x, candidate_y)
            candidate = {
                "x": candidate_x,
                "y": candidate_y,
                "quality": score[0],
                "ringScore": score[1],
                "clutterScore": score[2],
            }
            candidates.append(candidate)
            if best == None or candidate["quality"] > best["quality"]:
                best = candidate
    second_quality = 0
    second_ring_score = 0
    for candidate in candidates:
        dx = candidate["x"] - best["x"]
        dy = candidate["y"] - best["y"]
        if dx * dx + dy * dy >= DISTINCT_CENTER_DISTANCE_SQUARED and candidate["quality"] > second_quality:
            second_quality = candidate["quality"]
            second_ring_score = candidate["ringScore"]
    result = base
    result["x"] = best["x"]
    result["y"] = best["y"]
    result["quality"] = best["quality"]
    result["ringScore"] = best["ringScore"]
    result["secondQuality"] = max(0, second_quality)
    result["secondRingScore"] = second_ring_score
    result["clutterScore"] = best["clutterScore"]
    result["centerMargin"] = best["quality"] - max(0, second_quality)
    if best["ringScore"] < MIN_RING_SCORE:
        return result
    if best["quality"] <= 0:
        result["reason"] = "RADIAL_CONTRAST_NOT_CONFIRMED"
        return result
    if second_quality > 0 and best["quality"] - second_quality <= AMBIGUOUS_QUALITY_MARGIN:
        result["reason"] = "DISTINCT_CENTER_AMBIGUOUS"
        return result
    shape = topology(points, best["x"], best["y"])
    result["occupiedAngularBins"] = shape[0]
    result["angularTransitions"] = shape[1]
    result["angularRuns"] = shape[2]
    result["structuralArcOccupiedBins"] = shape[3]
    result["labelSectorOccupiedBins"] = shape[4]
    if shape[1] < 2:
        result["reason"] = "RETICLE_GAP_NOT_CONFIRMED"
        return result
    result["presentation"] = "DASHED" if shape[2] >= MIN_DASHED_ANGULAR_RUNS else "SOLID"
    minimum_occupied = MIN_DASHED_OCCUPIED_ANGULAR_BINS if result["presentation"] == "DASHED" else MIN_SOLID_OCCUPIED_ANGULAR_BINS
    if shape[0] < minimum_occupied:
        result["reason"] = result["presentation"] + "_ANGULAR_COVERAGE_LOW"
        return result
    confidence = shape_confidence(best, second_quality, shape)
    result["shapeConfidencePermille"] = confidence[0]
    result["structuralCoveragePermille"] = confidence[1]
    result["labelGapClarityPermille"] = confidence[2]
    result["radialContrastPermille"] = confidence[3]
    result["centerUniquenessPermille"] = confidence[4]
    result["state"] = "VIABLE"
    result["reason"] = "THREE_QUARTER_FOCUS_FRAME_CONFIRMED"
    return result

def public_plane(plane, roi_x, roi_y):
    return {
        "name": plane["name"],
        "state": plane["state"],
        "reason": plane["reason"],
        "thresholdMethod": plane["thresholdMethod"],
        "threshold": plane["threshold"],
        "nonzeroCount": plane["nonzeroCount"],
        "nonzeroRatioPermille": plane["nonzeroRatioPermille"],
        "thresholdDegenerate": plane["thresholdDegenerate"],
        "pixelCount": plane["pixelCount"],
        "referenceX": roi_x + plane["x"],
        "referenceY": roi_y + plane["y"],
        "quality": plane["quality"],
        "ringScore": plane["ringScore"],
        "secondQuality": plane["secondQuality"],
        "clutterScore": plane["clutterScore"],
        "centerMargin": plane["centerMargin"],
        "occupiedAngularBins": plane["occupiedAngularBins"],
        "angularTransitions": plane["angularTransitions"],
        "angularRuns": plane["angularRuns"],
        "structuralArcOccupiedBins": plane["structuralArcOccupiedBins"],
        "labelSectorOccupiedBins": plane["labelSectorOccupiedBins"],
        "structuralCoveragePermille": plane["structuralCoveragePermille"],
        "labelGapClarityPermille": plane["labelGapClarityPermille"],
        "radialContrastPermille": plane["radialContrastPermille"],
        "centerUniquenessPermille": plane["centerUniquenessPermille"],
        "shapeConfidencePermille": plane["shapeConfidencePermille"],
        "presentation": plane["presentation"],
    }

def unknown(reason, planes, sample, roi_x, roi_y, evidence_policy):
    public_planes = []
    for plane in planes:
        public_planes.append(public_plane(plane, roi_x, roi_y))
    return {
        "schemaVersion": 1,
        "target": {
            "state": "UNKNOWN", "referenceX": None, "referenceY": None,
            "offsetX": None, "offsetY": None, "centerDistancePixels": None,
            "reason": reason, "bestScore": 0, "secondScore": 0,
            "presentation": None, "occupiedAngularBins": 0,
            "angularRuns": 0, "evidencePlane": None, "evidenceQuality": None,
            "shapeConfidencePermille": None,
        },
        "evidence": {
            "region": sample["region"], "physicalRegion": sample["physicalRegion"],
            "capturedAt": sample["frame"]["capturedAt"], "selectedPlane": None,
            "requestedPolicy": evidence_policy,
            "selectionReason": reason, "planes": public_planes,
        },
    }

def main(ctx):
    hint_x = ctx.inputs["hintX"]
    hint_y = ctx.inputs["hintY"]
    evidence_policy = ctx.inputs.get("evidencePolicy", "ADAPTIVE_ORANGE")
    roi_x = hint_x - ROI_HALF
    roi_y = hint_y - ROI_HALF
    sample = observer.screen.read_region(x=roi_x, y=roi_y, w=ROI_SIZE, h=ROI_SIZE, sampling="reference")
    image = sample["image"]
    if sample["sampling"] != "reference" or sample["coordinateSpace"]["width"] != REFERENCE_WIDTH or sample["coordinateSpace"]["height"] != REFERENCE_HEIGHT or image["encoding"] != "rgb24-packed" or len(image["pixels"]) != ROI_SIZE * ROI_SIZE:
        return job.fail(code="SUPERCRUISE_RETICLE_EVIDENCE_INVALID", message="local reticle screen evidence is incomplete")

    strict_points = []
    opponent_scores = []
    hsv_scores = []
    opponent_histogram = []
    hsv_histogram = []
    for _ in range(256):
        opponent_histogram.append(0)
        hsv_histogram.append(0)
    for index in range(len(image["pixels"])):
        color = channels(image["pixels"][index])
        red = color[0]
        green = color[1]
        blue = color[2]
        if strict_rgb_orange(red, green, blue):
            strict_points.append([index % ROI_SIZE, index // ROI_SIZE])
        opponent = opponent_score(red, green, blue)
        hsv = hsv_orange_score(red, green, blue)
        opponent_scores.append(opponent)
        hsv_scores.append(hsv)
        opponent_histogram[opponent] += 1
        hsv_histogram[hsv] += 1

    opponent_threshold = otsu_threshold(opponent_histogram)
    hsv_threshold = otsu_threshold(hsv_histogram)
    strict_threshold = {
        "method": "FIXED_STRICT_RGB", "threshold": 1,
        "nonzeroCount": len(strict_points),
        "nonzeroRatioPermille": len(strict_points) * 1000 // (ROI_SIZE * ROI_SIZE),
        "degenerate": False,
    }
    planes = [
        evaluate_plane("STRICT_RGB", strict_points, strict_threshold),
        evaluate_plane("ORANGE_OPPONENT", points_at_or_above(opponent_scores, opponent_threshold["threshold"]), opponent_threshold),
        evaluate_plane("HSV_ORANGE", points_at_or_above(hsv_scores, hsv_threshold["threshold"]), hsv_threshold),
    ]
    adaptive_viable = []
    strict_viable = None
    for plane in planes:
        if plane["state"] != "VIABLE":
            continue
        selection_score = plane["shapeConfidencePermille"] * 1000 + min(999, plane["quality"])
        if plane["name"] == "STRICT_RGB":
            strict_viable = [selection_score, plane]
        else:
            adaptive_viable.append([selection_score, plane])
    viable = adaptive_viable
    selection_reason = "MAX_THREE_QUARTER_SHAPE_CONFIDENCE_THEN_RADIAL_QUALITY"
    if len(viable) == 0 and evidence_policy in ["OCCLUSION_AWARE", "HUD_OVERLAY_AWARE"] and strict_viable != None:
        viable = [strict_viable]
        selection_reason = evidence_policy + "_STRICT_RGB_SELECTED_AFTER_ADAPTIVE_REJECTION"
    if len(viable) == 0:
        return unknown("NO_EVIDENCE_PLANE_CONFIRMED_RETICLE", planes, sample, roi_x, roi_y, evidence_policy)
    selected = viable[0]
    runner_up = None
    for candidate in viable[1:]:
        if candidate[0] > selected[0]:
            runner_up = selected
            selected = candidate
        elif runner_up == None or candidate[0] > runner_up[0]:
            runner_up = candidate
    selected_plane = selected[1]
    if runner_up != None:
        other = runner_up[1]
        dx = selected_plane["x"] - other["x"]
        dy = selected_plane["y"] - other["y"]
        bin_difference = abs(selected_plane["occupiedAngularBins"] - other["occupiedAngularBins"])
        if dx * dx + dy * dy >= PLANE_DISAGREEMENT_DISTANCE_SQUARED and bin_difference <= PLANE_DISAGREEMENT_BIN_MARGIN:
            return unknown("EVIDENCE_PLANES_DISAGREE_ON_CENTER", planes, sample, roi_x, roi_y, evidence_policy)

    reference_x = roi_x + selected_plane["x"]
    reference_y = roi_y + selected_plane["y"]
    offset_x = reference_x - SCREEN_CENTER_X
    offset_y = reference_y - SCREEN_CENTER_Y
    public_planes = []
    for plane in planes:
        public_planes.append(public_plane(plane, roi_x, roi_y))
    return {
        "schemaVersion": 1,
        "target": {
            "state": "DETECTED", "referenceX": reference_x, "referenceY": reference_y,
            "offsetX": offset_x, "offsetY": offset_y,
            "centerDistancePixels": math.hypot(offset_x, offset_y),
            "reason": "CURRENT_FRAME_RETICLE_CONFIRMED:" + selected_plane["name"] + ":" + selected_plane["presentation"],
            "bestScore": selected_plane["ringScore"], "secondScore": selected_plane["secondRingScore"],
            "presentation": selected_plane["presentation"],
            "occupiedAngularBins": selected_plane["occupiedAngularBins"],
            "angularRuns": selected_plane["angularRuns"],
            "evidencePlane": selected_plane["name"], "evidenceQuality": selected[0],
            "shapeConfidencePermille": selected_plane["shapeConfidencePermille"],
        },
        "evidence": {
            "region": sample["region"], "physicalRegion": sample["physicalRegion"],
            "capturedAt": sample["frame"]["capturedAt"], "selectedPlane": selected_plane["name"],
            "requestedPolicy": evidence_policy, "selectionReason": selection_reason,
            "planes": public_planes,
        },
    }
