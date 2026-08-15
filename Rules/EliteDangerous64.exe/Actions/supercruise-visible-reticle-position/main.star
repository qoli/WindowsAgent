REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
SCREEN_CENTER_X = 960
SCREEN_CENTER_Y = 540
ROI_SIZE = 140
ROI_HALF = 70
CANDIDATE_SPAN = 28
CANDIDATE_STEP = 4
REFINEMENT_RADIUS = 4
SEARCH_INNER_RADIUS = 40
SEARCH_OUTER_RADIUS = 52
SEARCH_INNER_RADIUS_SQUARED = SEARCH_INNER_RADIUS * SEARCH_INNER_RADIUS
SEARCH_OUTER_RADIUS_SQUARED = SEARCH_OUTER_RADIUS * SEARCH_OUTER_RADIUS
INNER_NOISE_MIN_RADIUS_SQUARED = 28 * 28
INNER_NOISE_MAX_RADIUS_SQUARED = 38 * 38
OUTER_NOISE_MIN_RADIUS_SQUARED = 54 * 54
OUTER_NOISE_MAX_RADIUS_SQUARED = 64 * 64
RIDGE_SIGNAL_HALF_WIDTH = 1
RIDGE_BACKGROUND_INNER_OFFSET = 5
RIDGE_BACKGROUND_OUTER_OFFSET = 5
MIN_STRUCTURAL_OCCUPIED_BINS = 36
MAX_RADIUS_MAD = 2.0
MIN_ORIENTATION_COHERENCE_PERMILLE = 590
AMBIGUOUS_QUALITY_MARGIN = 10
DISTINCT_CENTER_DISTANCE_SQUARED = 20 * 20
PLANE_DISAGREEMENT_DISTANCE_SQUARED = 20 * 20
PLANE_DISAGREEMENT_BIN_MARGIN = 2
STRUCTURAL_ARC_BINS = 54
MIN_DASHED_STRUCTURAL_RUNS = 4
MIN_OTSU_NONZERO_PIXELS = 24
MIN_ALPHA_INVARIANT_ORANGE_SCORE = 80
MAX_EVIDENCE_PLANE_PIXELS = 6000
MAX_CANDIDATE_SCORE_POINTS = 1200
MIN_CIRCLE_FIT_POINTS = 36
CIRCLE_FIT_OUTLIER_FLOOR_PIXELS = 1.5
CIRCLE_FIT_OUTLIER_MAD_MULTIPLIER = 3.0
MAX_CENTER_COVARIANCE_TRACE = 4.0
RUNNER_UP_SEED_DISTANCE_SQUARED = 8 * 8
RUNNER_UP_MODE_DISTANCE_SQUARED = 6 * 6
RUNNER_UP_SUPPORT_MARGIN = 2
RUNNER_UP_RESIDUAL_MARGIN_PIXELS = 1.5
MAX_RUNNER_UP_SEEDS = 6

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
    # Hue affinity and saturation are invariant under uniform alpha/value
    # scaling. Value is deliberately excluded: the selected-target reticle may
    # be dimmed by the HUD compositor without changing its orange identity.
    return int(255.0 * affinity * saturation)

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

def candidate_score(points, point_weight, candidate_x, candidate_y):
    ring_score = 0
    clutter_score = 0
    for point in points:
        dx = point[0] - candidate_x
        dy = point[1] - candidate_y
        radius_squared = dx * dx + dy * dy
        if radius_squared >= SEARCH_INNER_RADIUS_SQUARED and radius_squared <= SEARCH_OUTER_RADIUS_SQUARED:
            ring_score += point_weight
        elif (radius_squared >= INNER_NOISE_MIN_RADIUS_SQUARED and radius_squared <= INNER_NOISE_MAX_RADIUS_SQUARED) or (radius_squared >= OUTER_NOISE_MIN_RADIUS_SQUARED and radius_squared <= OUTER_NOISE_MAX_RADIUS_SQUARED):
            clutter_score += point_weight
    hint_distance = abs(candidate_x - ROI_HALF) + abs(candidate_y - ROI_HALF)
    return [ring_score * 5 - clutter_score * 7 - hint_distance, ring_score, clutter_score]

def score_at(scores, x, y):
    sample_x = int(x + 0.5)
    sample_y = int(y + 0.5)
    if sample_x < 1 or sample_x >= ROI_SIZE - 1 or sample_y < 1 or sample_y >= ROI_SIZE - 1:
        return 0
    return scores[sample_y * ROI_SIZE + sample_x]

def radial_orientation_coherence(scores, center_x, center_y, cosine, sine, radius):
    x = float(center_x) + cosine * float(radius)
    y = float(center_y) + sine * float(radius)
    gradient_x = float(score_at(scores, x + 1.0, y) - score_at(scores, x - 1.0, y))
    gradient_y = float(score_at(scores, x, y + 1.0) - score_at(scores, x, y - 1.0))
    magnitude = math.hypot(gradient_x, gradient_y)
    if magnitude <= 0.0:
        return 0
    return int(abs(gradient_x * cosine + gradient_y * sine) * 1000.0 / magnitude)

def median(values):
    ordered = sorted(values)
    middle = len(ordered) // 2
    if len(ordered) % 2 == 1:
        return float(ordered[middle])
    return float(ordered[middle - 1] + ordered[middle]) / 2.0

def structural_directions():
    directions = []
    for structural_bin in range(STRUCTURAL_ARC_BINS):
        angle = math.pi / 4.0 + (float(structural_bin) + 0.5) * (3.0 * math.pi / 2.0) / float(STRUCTURAL_ARC_BINS)
        directions.append([math.cos(angle), math.sin(angle)])
    return directions

def high_confidence_arc_points(scores, threshold, candidate_x, candidate_y, directions):
    points = []
    for direction in directions:
        cosine = direction[0]
        sine = direction[1]
        best = None
        for radius in range(SEARCH_INNER_RADIUS, SEARCH_OUTER_RADIUS + 1):
            signal = 0
            peak = 0
            for offset in range(-RIDGE_SIGNAL_HALF_WIDTH, RIDGE_SIGNAL_HALF_WIDTH + 1):
                sample_score = score_at(scores, float(candidate_x) + cosine * float(radius + offset), float(candidate_y) + sine * float(radius + offset))
                signal += sample_score
                peak = max(peak, sample_score)
            signal = signal // (RIDGE_SIGNAL_HALF_WIDTH * 2 + 1)
            inner_background = score_at(scores, float(candidate_x) + cosine * float(radius - RIDGE_BACKGROUND_INNER_OFFSET), float(candidate_y) + sine * float(radius - RIDGE_BACKGROUND_INNER_OFFSET))
            outer_background = score_at(scores, float(candidate_x) + cosine * float(radius + RIDGE_BACKGROUND_OUTER_OFFSET), float(candidate_y) + sine * float(radius + RIDGE_BACKGROUND_OUTER_OFFSET))
            contrast = signal - (inner_background + outer_background) // 2
            if best == None or contrast > best[0]:
                best = [contrast, peak, radius]
        if best[0] > 0 and best[1] >= threshold:
            points.append([
                float(candidate_x) + cosine * float(best[2]),
                float(candidate_y) + sine * float(best[2]),
            ])
    return points

def algebraic_circle_fit(points):
    if len(points) < 3:
        return None
    mean_x = 0.0
    mean_y = 0.0
    for point in points:
        mean_x += point[0]
        mean_y += point[1]
    mean_x = mean_x / float(len(points))
    mean_y = mean_y / float(len(points))
    sum_xx = 0.0
    sum_xy = 0.0
    sum_yy = 0.0
    sum_xq = 0.0
    sum_yq = 0.0
    for point in points:
        x = point[0] - mean_x
        y = point[1] - mean_y
        q = x * x + y * y
        sum_xx += x * x
        sum_xy += x * y
        sum_yy += y * y
        sum_xq += x * q
        sum_yq += y * q
    determinant = sum_xx * sum_yy - sum_xy * sum_xy
    if determinant <= 0.000001:
        return None
    slope_x = (sum_xq * sum_yy - sum_yq * sum_xy) / determinant
    slope_y = (sum_yq * sum_xx - sum_xq * sum_xy) / determinant
    center_x = mean_x + slope_x / 2.0
    center_y = mean_y + slope_y / 2.0
    radii = []
    for point in points:
        radii.append(math.hypot(point[0] - center_x, point[1] - center_y))
    return {"centerX": center_x, "centerY": center_y, "radius": median(radii)}

def circle_fit_residuals(points, fit):
    residuals = []
    for point in points:
        residuals.append(abs(math.hypot(point[0] - fit["centerX"], point[1] - fit["centerY"]) - fit["radius"]))
    return residuals

def percentile95(values):
    if len(values) == 0:
        return 99.0
    ordered = sorted(values)
    index = (len(ordered) * 95 + 99) // 100 - 1
    return float(ordered[index])

def robust_circle_fit(scores, threshold, candidate_x, candidate_y, directions):
    points = high_confidence_arc_points(scores, threshold, candidate_x, candidate_y, directions)
    rejected = {
        "valid": False, "pointCount": len(points), "inlierCount": 0,
        "centerX": float(candidate_x), "centerY": float(candidate_y),
        "radius": 0.0, "residualP95": 99.0,
        "covarianceXX": 99.0, "covarianceXY": 0.0, "covarianceYY": 99.0,
        "covarianceTrace": 198.0,
    }
    if len(points) < MIN_CIRCLE_FIT_POINTS:
        return rejected
    initial = algebraic_circle_fit(points)
    if initial == None:
        return rejected
    initial_residuals = circle_fit_residuals(points, initial)
    residual_median = median(initial_residuals)
    deviations = []
    for residual in initial_residuals:
        deviations.append(abs(residual - residual_median))
    residual_mad = median(deviations)
    inlier_limit = max(CIRCLE_FIT_OUTLIER_FLOOR_PIXELS, residual_median + CIRCLE_FIT_OUTLIER_MAD_MULTIPLIER * residual_mad)
    inliers = []
    for index in range(len(points)):
        if initial_residuals[index] <= inlier_limit:
            inliers.append(points[index])
    if len(inliers) < MIN_CIRCLE_FIT_POINTS:
        return rejected
    fit = algebraic_circle_fit(inliers)
    if fit == None:
        return rejected
    residuals = circle_fit_residuals(inliers, fit)
    sum_jxx = 0.0
    sum_jxy = 0.0
    sum_jyy = 0.0
    residual_squared_sum = 0.0
    for index in range(len(inliers)):
        dx = inliers[index][0] - fit["centerX"]
        dy = inliers[index][1] - fit["centerY"]
        distance = math.hypot(dx, dy)
        if distance <= 0.0:
            continue
        unit_x = dx / distance
        unit_y = dy / distance
        sum_jxx += unit_x * unit_x
        sum_jxy += unit_x * unit_y
        sum_jyy += unit_y * unit_y
        residual_squared_sum += residuals[index] * residuals[index]
    determinant = sum_jxx * sum_jyy - sum_jxy * sum_jxy
    if determinant <= 0.000001:
        return rejected
    residual_variance = residual_squared_sum / float(max(1, len(inliers) - 3))
    covariance_xx = residual_variance * sum_jyy / determinant
    covariance_xy = -residual_variance * sum_jxy / determinant
    covariance_yy = residual_variance * sum_jxx / determinant
    return {
        "valid": True, "pointCount": len(points), "inlierCount": len(inliers),
        "centerX": fit["centerX"], "centerY": fit["centerY"],
        "radius": fit["radius"], "residualP95": percentile95(residuals),
        "covarianceXX": covariance_xx, "covarianceXY": covariance_xy,
        "covarianceYY": covariance_yy,
        "covarianceTrace": covariance_xx + covariance_yy,
    }

def polar_candidate(scores, alpha_scores, threshold, candidate_x, candidate_y, directions):
    occupied_bins = []
    radii = []
    coherences = []
    ridge_score = 0
    clutter_score = 0
    alpha_score = 0
    # A true 270-degree arc: 54 five-degree bins from +45 through +315
    # degrees. The omitted right-facing quarter is the target-label opening.
    for direction in directions:
        cosine = direction[0]
        sine = direction[1]
        best = None
        for radius in range(SEARCH_INNER_RADIUS, SEARCH_OUTER_RADIUS + 1):
            signal = 0
            peak = 0
            for offset in range(-RIDGE_SIGNAL_HALF_WIDTH, RIDGE_SIGNAL_HALF_WIDTH + 1):
                sample_score = score_at(scores, float(candidate_x) + cosine * float(radius + offset), float(candidate_y) + sine * float(radius + offset))
                signal += sample_score
                peak = max(peak, sample_score)
            signal = signal // (RIDGE_SIGNAL_HALF_WIDTH * 2 + 1)
            inner_background = score_at(scores, float(candidate_x) + cosine * float(radius - RIDGE_BACKGROUND_INNER_OFFSET), float(candidate_y) + sine * float(radius - RIDGE_BACKGROUND_INNER_OFFSET))
            outer_background = score_at(scores, float(candidate_x) + cosine * float(radius + RIDGE_BACKGROUND_OUTER_OFFSET), float(candidate_y) + sine * float(radius + RIDGE_BACKGROUND_OUTER_OFFSET))
            local_background = (inner_background + outer_background) // 2
            contrast = signal - local_background
            if best == None or contrast > best[0]:
                best = [contrast, signal, local_background, radius, peak]
        occupied = best[0] > 0 and best[4] >= threshold
        occupied_bins.append(occupied)
        if not occupied:
            continue
        radii.append(best[3])
        ridge_score += best[0]
        clutter_score += best[2]
        alpha_score += score_at(alpha_scores, float(candidate_x) + cosine * float(best[3]), float(candidate_y) + sine * float(best[3]))
        inner_coherence = radial_orientation_coherence(scores, candidate_x, candidate_y, cosine, sine, best[3] - 2)
        outer_coherence = radial_orientation_coherence(scores, candidate_x, candidate_y, cosine, sine, best[3] + 2)
        coherences.append(max(inner_coherence, outer_coherence))

    runs = 0
    in_run = False
    for occupied in occupied_bins:
        if occupied and not in_run:
            runs += 1
        in_run = occupied
    coverage = len(radii)
    coverage_permille = coverage * 1000 // STRUCTURAL_ARC_BINS
    radius_mad = 99.0
    radius_consistency = 0
    orientation_coherence = 0
    mean_alpha_score = 0
    if coverage > 0:
        radius_median = median(radii)
        deviations = []
        for radius in radii:
            deviations.append(abs(float(radius) - radius_median))
        radius_mad = median(deviations)
        radius_consistency = clamp_permille(1000 - int(radius_mad * 250.0))
        coherence_sum = 0
        for coherence in coherences:
            coherence_sum += coherence
        orientation_coherence = coherence_sum // len(coherences)
        mean_alpha_score = alpha_score // coverage
    preliminary_confidence = (coverage_permille * 50 + radius_consistency * 25 + orientation_coherence * 20) // 95
    hint_distance = abs(candidate_x - ROI_HALF) + abs(candidate_y - ROI_HALF)
    return {
        "x": candidate_x, "y": candidate_y,
        "quality": preliminary_confidence * 100 - hint_distance,
        "ringScore": ridge_score, "clutterScore": clutter_score,
        "structuralArcOccupiedBins": coverage,
        "structuralCoveragePermille": coverage_permille,
        "radiusMAD": radius_mad,
        "radiusConsistencyPermille": radius_consistency,
        "orientationCoherence": orientation_coherence,
        "alphaInvariantOrangeScore": mean_alpha_score,
        "angularRuns": runs,
    }

def clamp_permille(value):
    if value < 0:
        return 0
    if value > 1000:
        return 1000
    return value

def evaluate_plane(name, scores, alpha_scores, threshold_evidence, directions):
    threshold = threshold_evidence["threshold"]
    base = {
        "name": name, "state": "REJECTED", "reason": "RING_SCORE_LOW",
        "thresholdMethod": threshold_evidence["method"],
        "threshold": threshold,
        "nonzeroCount": threshold_evidence["nonzeroCount"],
        "nonzeroRatioPermille": threshold_evidence["nonzeroRatioPermille"],
        "thresholdDegenerate": threshold_evidence["degenerate"],
        "pixelCount": 0, "x": ROI_HALF, "y": ROI_HALF,
        "quality": 0, "ringScore": 0, "secondQuality": 0, "secondRingScore": 0,
        "clutterScore": 0, "centerMargin": 0,
        "occupiedAngularBins": 0, "angularTransitions": 0,
        "angularRuns": 0, "structuralArcOccupiedBins": 0,
        "labelSectorOccupiedBins": 0, "structuralCoveragePermille": 0,
        "labelGapClarityPermille": 0, "radialContrastPermille": 0,
        "centerUniquenessPermille": 0, "shapeConfidencePermille": 0,
        "alphaInvariantOrangeScore": 0, "structuralCoverage": 0.0,
        "radiusMAD": 99.0, "orientationCoherence": 0.0,
        "arcConfidencePermille": 0,
        "circleFitPointCount": 0, "circleFitInlierCount": 0,
        "circleFitResidualP95": None,
        "centerCovarianceXX": None, "centerCovarianceXY": None,
        "centerCovarianceYY": None, "centerCovarianceTrace": None,
        "runnerUpReferenceX": None, "runnerUpReferenceY": None,
        "runnerUpSupport": 0, "runnerUpResidualP95": None,
        "runnerUpCenterDistancePixels": None, "centerModeCount": 0,
        "presentation": None,
    }
    if threshold_evidence["degenerate"]:
        base["reason"] = "HISTOGRAM_DEGENERATE"
        return base
    points = points_at_or_above(scores, threshold)
    base["pixelCount"] = len(points)
    if len(points) > MAX_EVIDENCE_PLANE_PIXELS:
        base["reason"] = "PIXEL_DENSITY_HIGH"
        return base
    score_points = points
    point_weight = 1
    if len(points) > MAX_CANDIDATE_SCORE_POINTS:
        point_weight = (len(points) + MAX_CANDIDATE_SCORE_POINTS - 1) // MAX_CANDIDATE_SCORE_POINTS
        score_points = []
        for index in range(0, len(points), point_weight):
            score_points.append(points[index])
    candidates = []
    coarse_candidates = []
    coarse_best = None
    for candidate_y in range(ROI_HALF - CANDIDATE_SPAN, ROI_HALF + CANDIDATE_SPAN + 1, CANDIDATE_STEP):
        for candidate_x in range(ROI_HALF - CANDIDATE_SPAN, ROI_HALF + CANDIDATE_SPAN + 1, CANDIDATE_STEP):
            score = candidate_score(score_points, point_weight, candidate_x, candidate_y)
            candidate = {"x": candidate_x, "y": candidate_y, "quality": score[0], "ringScore": score[1], "clutterScore": score[2]}
            candidates.append(candidate)
            coarse_candidates.append(candidate)
            if coarse_best == None or candidate["quality"] > coarse_best["quality"]:
                coarse_best = candidate
    # The four-pixel grid bounds global search cost. A fixed one-pixel local
    # refinement removes the measured hint/grid phase flicker without changing
    # capture, ROI, evidence plane, or frame.
    best = None
    for candidate_y in range(coarse_best["y"] - REFINEMENT_RADIUS, coarse_best["y"] + REFINEMENT_RADIUS + 1):
        for candidate_x in range(coarse_best["x"] - REFINEMENT_RADIUS, coarse_best["x"] + REFINEMENT_RADIUS + 1):
            score = candidate_score(score_points, point_weight, candidate_x, candidate_y)
            candidate = {"x": candidate_x, "y": candidate_y, "quality": score[0], "ringScore": score[1], "clutterScore": score[2]}
            candidates.append(candidate)
            if best == None or candidate["quality"] > best["quality"]:
                best = candidate
    fit = robust_circle_fit(scores, threshold, best["x"], best["y"], directions)
    result = base
    result["circleFitPointCount"] = fit["pointCount"]
    result["circleFitInlierCount"] = fit["inlierCount"]
    if not fit["valid"]:
        if fit["pointCount"] < MIN_STRUCTURAL_OCCUPIED_BINS:
            result["occupiedAngularBins"] = fit["pointCount"]
            result["structuralArcOccupiedBins"] = fit["pointCount"]
            result["structuralCoverage"] = float(fit["pointCount"]) / float(STRUCTURAL_ARC_BINS)
            result["structuralCoveragePermille"] = fit["pointCount"] * 1000 // STRUCTURAL_ARC_BINS
            result["reason"] = "STRUCTURAL_COVERAGE_LOW"
        else:
            result["reason"] = "ROBUST_CIRCLE_FIT_INSUFFICIENT"
        return result
    best["x"] = int(fit["centerX"] + 0.5)
    best["y"] = int(fit["centerY"] + 0.5)
    result["circleFitResidualP95"] = fit["residualP95"]
    result["centerCovarianceXX"] = fit["covarianceXX"]
    result["centerCovarianceXY"] = fit["covarianceXY"]
    result["centerCovarianceYY"] = fit["covarianceYY"]
    result["centerCovarianceTrace"] = fit["covarianceTrace"]
    result["centerModeCount"] = 1
    if fit["covarianceTrace"] > MAX_CENTER_COVARIANCE_TRACE:
        result["reason"] = "CENTER_COVARIANCE_HIGH"
        return result

    selected_runner_seeds = [coarse_best]
    runner_fit = None
    runner_fit_score = None
    for _ in range(MAX_RUNNER_UP_SEEDS):
        runner_seed = None
        for candidate in coarse_candidates:
            separated = True
            for selected_seed in selected_runner_seeds:
                seed_dx = candidate["x"] - selected_seed["x"]
                seed_dy = candidate["y"] - selected_seed["y"]
                if seed_dx * seed_dx + seed_dy * seed_dy < RUNNER_UP_SEED_DISTANCE_SQUARED:
                    separated = False
            if separated and (runner_seed == None or candidate["quality"] > runner_seed["quality"]):
                runner_seed = candidate
        if runner_seed == None:
            continue
        selected_runner_seeds.append(runner_seed)
        candidate_fit = robust_circle_fit(scores, threshold, runner_seed["x"], runner_seed["y"], directions)
        if not candidate_fit["valid"]:
            continue
        runner_x = int(candidate_fit["centerX"] + 0.5)
        runner_y = int(candidate_fit["centerY"] + 0.5)
        runner_dx = runner_x - best["x"]
        runner_dy = runner_y - best["y"]
        if runner_dx * runner_dx + runner_dy * runner_dy < RUNNER_UP_MODE_DISTANCE_SQUARED:
            continue
        candidate_fit_score = candidate_fit["inlierCount"] * 1000 - int(candidate_fit["residualP95"] * 100.0)
        if runner_fit == None or candidate_fit_score > runner_fit_score:
            runner_fit = candidate_fit
            runner_fit_score = candidate_fit_score
    if runner_fit != None:
        runner_x = int(runner_fit["centerX"] + 0.5)
        runner_y = int(runner_fit["centerY"] + 0.5)
        runner_dx = runner_x - best["x"]
        runner_dy = runner_y - best["y"]
        runner_distance = math.hypot(runner_dx, runner_dy)
        result["runnerUpReferenceX"] = runner_x
        result["runnerUpReferenceY"] = runner_y
        result["runnerUpSupport"] = runner_fit["inlierCount"]
        result["runnerUpResidualP95"] = runner_fit["residualP95"]
        result["runnerUpCenterDistancePixels"] = runner_distance
        if runner_fit["inlierCount"] >= fit["inlierCount"] - RUNNER_UP_SUPPORT_MARGIN and runner_fit["residualP95"] <= fit["residualP95"] + RUNNER_UP_RESIDUAL_MARGIN_PIXELS:
            result["centerModeCount"] = 2
            result["reason"] = "MULTIMODAL_CENTER_AMBIGUOUS"
            return result
    second_quality = 0
    second_ring_score = 0
    for candidate in candidates:
        dx = candidate["x"] - best["x"]
        dy = candidate["y"] - best["y"]
        if dx * dx + dy * dy >= DISTINCT_CENTER_DISTANCE_SQUARED and candidate["quality"] > second_quality:
            second_quality = candidate["quality"]
            second_ring_score = candidate["ringScore"]
    result["x"] = best["x"]
    result["y"] = best["y"]
    result["quality"] = best["quality"]
    result["ringScore"] = best["ringScore"]
    result["secondQuality"] = max(0, second_quality)
    result["secondRingScore"] = second_ring_score
    result["clutterScore"] = best["clutterScore"]
    result["centerMargin"] = best["quality"] - max(0, second_quality)
    if second_quality > 0 and best["quality"] - second_quality <= AMBIGUOUS_QUALITY_MARGIN:
        result["reason"] = "DISTINCT_CENTER_AMBIGUOUS"
        return result
    polar = polar_candidate(scores, alpha_scores, threshold, best["x"], best["y"], directions)
    result["occupiedAngularBins"] = polar["structuralArcOccupiedBins"]
    result["angularTransitions"] = polar["angularRuns"] * 2
    result["angularRuns"] = polar["angularRuns"]
    result["structuralArcOccupiedBins"] = polar["structuralArcOccupiedBins"]
    result["structuralCoverage"] = float(polar["structuralArcOccupiedBins"]) / float(STRUCTURAL_ARC_BINS)
    result["structuralCoveragePermille"] = polar["structuralCoveragePermille"]
    result["labelSectorOccupiedBins"] = 0
    result["labelGapClarityPermille"] = 1000
    result["radiusMAD"] = polar["radiusMAD"]
    result["orientationCoherence"] = float(polar["orientationCoherence"]) / 1000.0
    result["alphaInvariantOrangeScore"] = polar["alphaInvariantOrangeScore"]
    if polar["structuralArcOccupiedBins"] < MIN_STRUCTURAL_OCCUPIED_BINS:
        result["reason"] = "STRUCTURAL_COVERAGE_LOW"
        return result
    result["presentation"] = "DASHED" if polar["angularRuns"] >= MIN_DASHED_STRUCTURAL_RUNS else "SOLID"
    if polar["radiusMAD"] > MAX_RADIUS_MAD:
        result["reason"] = "RADIUS_MAD_HIGH"
        return result
    if polar["orientationCoherence"] < MIN_ORIENTATION_COHERENCE_PERMILLE:
        result["reason"] = "ORIENTATION_COHERENCE_LOW"
        return result
    centre_margin = best["quality"] if second_quality <= 0 else best["quality"] - second_quality
    centre_uniqueness = clamp_permille(centre_margin * 1000 // max(1, abs(best["quality"])))
    radial_contrast = clamp_permille(best["ringScore"] * 1000 // (best["ringScore"] + best["clutterScore"] + 1))
    confidence = (polar["structuralCoveragePermille"] * 50 + polar["radiusConsistencyPermille"] * 25 + polar["orientationCoherence"] * 20 + centre_uniqueness * 5) // 100
    result["arcConfidencePermille"] = confidence
    result["shapeConfidencePermille"] = confidence
    result["radialContrastPermille"] = radial_contrast
    result["centerUniquenessPermille"] = centre_uniqueness
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
        "alphaInvariantOrangeScore": plane["alphaInvariantOrangeScore"],
        "structuralCoverage": plane["structuralCoverage"],
        "radiusMAD": plane["radiusMAD"],
        "orientationCoherence": plane["orientationCoherence"],
        "arcConfidencePermille": plane["arcConfidencePermille"],
        "circleFitPointCount": plane["circleFitPointCount"],
        "circleFitInlierCount": plane["circleFitInlierCount"],
        "circleFitResidualP95": plane["circleFitResidualP95"],
        "centerCovarianceXX": plane["centerCovarianceXX"],
        "centerCovarianceXY": plane["centerCovarianceXY"],
        "centerCovarianceYY": plane["centerCovarianceYY"],
        "centerCovarianceTrace": plane["centerCovarianceTrace"],
        "runnerUpReferenceX": None if plane["runnerUpReferenceX"] == None else roi_x + plane["runnerUpReferenceX"],
        "runnerUpReferenceY": None if plane["runnerUpReferenceY"] == None else roi_y + plane["runnerUpReferenceY"],
        "runnerUpSupport": plane["runnerUpSupport"],
        "runnerUpResidualP95": plane["runnerUpResidualP95"],
        "runnerUpCenterDistancePixels": plane["runnerUpCenterDistancePixels"],
        "centerModeCount": plane["centerModeCount"],
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
            "shapeConfidencePermille": None, "alphaInvariantOrangeScore": None,
            "structuralCoverage": None, "radiusMAD": None,
            "orientationCoherence": None, "arcConfidencePermille": None,
            "circleFitPointCount": None, "circleFitInlierCount": None,
            "circleFitResidualP95": None,
            "centerCovarianceXX": None, "centerCovarianceXY": None,
            "centerCovarianceYY": None, "centerCovarianceTrace": None,
            "runnerUpReferenceX": None, "runnerUpReferenceY": None,
            "runnerUpSupport": None, "runnerUpResidualP95": None,
            "runnerUpCenterDistancePixels": None, "centerModeCount": None,
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

    strict_scores = []
    strict_count = 0
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
            strict_scores.append(255)
            strict_count += 1
        else:
            strict_scores.append(0)
        opponent = opponent_score(red, green, blue)
        hsv = hsv_orange_score(red, green, blue)
        opponent_scores.append(opponent)
        hsv_scores.append(hsv)
        opponent_histogram[opponent] += 1
        hsv_histogram[hsv] += 1

    opponent_threshold = otsu_threshold(opponent_histogram)
    alpha_nonzero_count = 0
    for score in range(1, 256):
        alpha_nonzero_count += hsv_histogram[score]
    hsv_threshold = {
        "method": "FIXED_ALPHA_INVARIANT", "threshold": MIN_ALPHA_INVARIANT_ORANGE_SCORE,
        "nonzeroCount": alpha_nonzero_count,
        "nonzeroRatioPermille": alpha_nonzero_count * 1000 // (ROI_SIZE * ROI_SIZE),
        "degenerate": alpha_nonzero_count < MIN_OTSU_NONZERO_PIXELS,
    }
    strict_threshold = {
        "method": "FIXED_STRICT_RGB", "threshold": 1,
        "nonzeroCount": strict_count,
        "nonzeroRatioPermille": strict_count * 1000 // (ROI_SIZE * ROI_SIZE),
        "degenerate": False,
    }
    directions = structural_directions()
    planes = [
        evaluate_plane("STRICT_RGB", strict_scores, hsv_scores, strict_threshold, directions),
        evaluate_plane("ORANGE_OPPONENT", opponent_scores, hsv_scores, opponent_threshold, directions),
        evaluate_plane("HSV_ORANGE", hsv_scores, hsv_scores, hsv_threshold, directions),
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
            "alphaInvariantOrangeScore": selected_plane["alphaInvariantOrangeScore"],
            "structuralCoverage": selected_plane["structuralCoverage"],
            "radiusMAD": selected_plane["radiusMAD"],
            "orientationCoherence": selected_plane["orientationCoherence"],
            "arcConfidencePermille": selected_plane["arcConfidencePermille"],
            "circleFitPointCount": selected_plane["circleFitPointCount"],
            "circleFitInlierCount": selected_plane["circleFitInlierCount"],
            "circleFitResidualP95": selected_plane["circleFitResidualP95"],
            "centerCovarianceXX": selected_plane["centerCovarianceXX"],
            "centerCovarianceXY": selected_plane["centerCovarianceXY"],
            "centerCovarianceYY": selected_plane["centerCovarianceYY"],
            "centerCovarianceTrace": selected_plane["centerCovarianceTrace"],
            "runnerUpReferenceX": None if selected_plane["runnerUpReferenceX"] == None else roi_x + selected_plane["runnerUpReferenceX"],
            "runnerUpReferenceY": None if selected_plane["runnerUpReferenceY"] == None else roi_y + selected_plane["runnerUpReferenceY"],
            "runnerUpSupport": selected_plane["runnerUpSupport"],
            "runnerUpResidualP95": selected_plane["runnerUpResidualP95"],
            "runnerUpCenterDistancePixels": selected_plane["runnerUpCenterDistancePixels"],
            "centerModeCount": selected_plane["centerModeCount"],
        },
        "evidence": {
            "region": sample["region"], "physicalRegion": sample["physicalRegion"],
            "capturedAt": sample["frame"]["capturedAt"], "selectedPlane": selected_plane["name"],
            "requestedPolicy": evidence_policy, "selectionReason": selection_reason,
            "planes": public_planes,
        },
    }
