REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
ROI_X = 682
ROI_Y = 771
ROI_WIDTH = 96
ROI_HEIGHT = 96
WIDE_ROI_X = ROI_X - 48
WIDE_ROI_Y = ROI_Y - 48
WIDE_ROI_WIDTH = 192
WIDE_ROI_HEIGHT = 192
MIN_ORANGE_PIXELS_REFERENCE = 150
CENTER_ZONE_RADIUS = 4
OUTPUT_SCALE = 1000.0

HOUGH_RADII = [24, 26, 28, 30, 32, 34]
FAST_CENTER_MIN = 42
FAST_CENTER_MAX_EXCLUSIVE = 55
WIDE_CENTER_MIN = 48
WIDE_CENTER_MAX_EXCLUSIVE = 145
HOUGH_COS = [1.0, 0.966, 0.866, 0.707, 0.5, 0.259, 0.0, -0.259, -0.5, -0.707, -0.866, -0.966, -1.0, -0.966, -0.866, -0.707, -0.5, -0.259, 0.0, 0.259, 0.5, 0.707, 0.866, 0.966]
HOUGH_SIN = [0.0, 0.259, 0.5, 0.707, 0.866, 0.966, 1.0, 0.966, 0.866, 0.707, 0.5, 0.259, 0.0, -0.259, -0.5, -0.707, -0.866, -0.966, -1.0, -0.966, -0.866, -0.707, -0.5, -0.259]
HOUGH_MAX_EDGE_SAMPLES = 420
HOUGH_MIN_VOTES = 4
HOUGH_MAX_CANDIDATES = 8
HOUGH_EDGE_OFFSET = 5
HOUGH_MIN_ANGULAR_COVERAGE = 16
HOUGH_MIN_EDGE_VOTES = 8

STRICT_THRESHOLDS = [128]
OPPONENT_THRESHOLDS = [2, 4, 6, 8, 10, 12, 16, 20, 24, 32, 40, 48, 64]
STRICT_SOLID_THRESHOLD = 3.8
STRICT_HOLLOW_THRESHOLD = 3.0
STRICT_MARGIN = 0.5
OPPONENT_SOLID_THRESHOLD = 3.0
OPPONENT_HOLLOW_THRESHOLD = 3.0
OPPONENT_MARGIN = 0.0
CASCADE_MIN_CONFIDENCE = 0.5
REFERENCE_FAST_PATH_MIN_CONFIDENCE = 0.5

def round_output(value):
    return math.round(value * OUTPUT_SCALE) / OUTPUT_SCALE

def is_orange(red, green, blue):
    return red >= 170 and green >= 55 and green <= 210 and blue <= 110 and red >= green + 35

def pixel_channels(pixel):
    return [pixel // 65536, (pixel // 256) % 256, pixel % 256]

def build_responses(pixels):
    orange = []
    strict = []
    opponent = []
    orange_count = 0
    for pixel in pixels:
        channels = pixel_channels(pixel)
        red = channels[0]
        green = channels[1]
        blue = channels[2]
        orange_pixel = is_orange(red, green, blue)
        orange.append(orange_pixel)
        if orange_pixel:
            orange_count += 1
        cyan_floor = min(green, blue)
        strict.append(255 if cyan_floor >= 100 and cyan_floor >= red + 12 and abs(green - blue) <= 48 else 0)
        opponent.append(max(0, cyan_floor - red))
    return {
        "orange": orange,
        "orangeCount": orange_count,
        "strict": strict,
        "opponent": opponent,
    }

def native_mask_to_reference(mask, native_width, native_height):
    reference = []
    for reference_y in range(ROI_HEIGHT):
        native_top = reference_y * native_height // ROI_HEIGHT
        native_bottom = max(native_top + 1, (reference_y + 1) * native_height // ROI_HEIGHT)
        for reference_x in range(ROI_WIDTH):
            native_left = reference_x * native_width // ROI_WIDTH
            native_right = max(native_left + 1, (reference_x + 1) * native_width // ROI_WIDTH)
            present = False
            for native_y in range(native_top, min(native_height, native_bottom)):
                for native_x in range(native_left, min(native_width, native_right)):
                    if mask[native_y * native_width + native_x]:
                        present = True
            reference.append(present)
    return reference

def evaluate_circle(mask, width, height, center_x, center_y, radius):
    angular_counts = []
    for _ in range(24):
        angular_counts.append(0)
    annulus_orange = 0
    annulus_area = 0
    for y in range(height):
        for x in range(width):
            delta_x = x - center_x
            delta_y = y - center_y
            distance = math.hypot(delta_x, delta_y)
            if abs(distance - radius) <= 3.0:
                annulus_area += 1
                if mask[y * width + x]:
                    annulus_orange += 1
                    angle = math.atan2(delta_y, delta_x)
                    if angle < 0:
                        angle += 2.0 * math.pi
                    angular_bin = min(23, int(angle * 24.0 / (2.0 * math.pi)))
                    angular_counts[angular_bin] += 1
    angular_coverage = 0
    for count in angular_counts:
        if count >= 2:
            angular_coverage += 1
    density = annulus_orange / float(max(1, annulus_area))
    return {
        "centerX": center_x,
        "centerY": center_y,
        "radius": radius,
        "angularCoverage": angular_coverage,
        "annulusDensity": density,
    }

def locate_compass_circle(reference_orange, width, height, native_orange_count, native_pixel_count, center_min, center_max_exclusive):
    candidate_count = 0
    ranked = []
    # The ROI is already a reviewed Compass-local crop. Search a bounded
    # center/radius parameter grid and vote on the annulus directly instead of
    # back-projecting every orange edge into a large Starlark dictionary.
    for center_y in range(center_min, center_max_exclusive, 3):
        for center_x in range(center_min, center_max_exclusive, 3):
            for radius in HOUGH_RADII:
                votes = 0
                edge_votes = 0
                for angle_index in range(24):
                    sample_x = int(math.round(center_x + radius * HOUGH_COS[angle_index]))
                    sample_y = int(math.round(center_y + radius * HOUGH_SIN[angle_index]))
                    if sample_x >= 0 and sample_x < width and sample_y >= 0 and sample_y < height and reference_orange[sample_y * width + sample_x]:
                        votes += 1
                        inner_x = int(math.round(center_x + (radius - HOUGH_EDGE_OFFSET) * HOUGH_COS[angle_index]))
                        inner_y = int(math.round(center_y + (radius - HOUGH_EDGE_OFFSET) * HOUGH_SIN[angle_index]))
                        outer_x = int(math.round(center_x + (radius + HOUGH_EDGE_OFFSET) * HOUGH_COS[angle_index]))
                        outer_y = int(math.round(center_y + (radius + HOUGH_EDGE_OFFSET) * HOUGH_SIN[angle_index]))
                        inner_orange = inner_x >= 0 and inner_x < width and inner_y >= 0 and inner_y < height and reference_orange[inner_y * width + inner_x]
                        outer_orange = outer_x >= 0 and outer_x < width and outer_y >= 0 and outer_y < height and reference_orange[outer_y * width + outer_x]
                        if not inner_orange or not outer_orange:
                            edge_votes += 1
                if votes < HOUGH_MIN_VOTES:
                    continue
                candidate_count += 1
                candidate = [edge_votes, votes, center_x, center_y, radius]
                if len(ranked) < HOUGH_MAX_CANDIDATES:
                    ranked.append(candidate)
                else:
                    weakest_index = 0
                    for index in range(1, len(ranked)):
                        if ranked[index][0] < ranked[weakest_index][0] or (ranked[index][0] == ranked[weakest_index][0] and ranked[index][1] < ranked[weakest_index][1]):
                            weakest_index = index
                    if edge_votes > ranked[weakest_index][0] or (edge_votes == ranked[weakest_index][0] and votes > ranked[weakest_index][1]):
                        ranked[weakest_index] = candidate

    evaluated = []
    for candidate in ranked:
        geometry = evaluate_circle(reference_orange, width, height, candidate[2], candidate[3], candidate[4])
        geometry["edgeVotes"] = candidate[0]
        geometry["votes"] = candidate[1]
        evaluated.append(geometry)

    orange_fraction = native_orange_count / float(max(1, native_pixel_count))
    selected = None
    for candidate in evaluated:
        if (
            candidate["angularCoverage"] >= HOUGH_MIN_ANGULAR_COVERAGE and
            candidate["edgeVotes"] >= HOUGH_MIN_EDGE_VOTES
        ):
            if selected == None or candidate["edgeVotes"] > selected["edgeVotes"] or (candidate["edgeVotes"] == selected["edgeVotes"] and candidate["votes"] > selected["votes"]):
                selected = candidate
    if selected == None:
        return {
            "visible": False,
            "circleCandidateCount": candidate_count,
            "houghPerimeterSamples": 24,
            "orangeFraction": orange_fraction,
            "centerX": None,
            "centerY": None,
            "radius": None,
            "angularCoverage": 0,
            "annulusDensity": None,
            "edgeVotes": None,
            "votes": None,
        }
    return {
        "visible": True,
        "circleCandidateCount": candidate_count,
        "houghPerimeterSamples": 24,
        "orangeFraction": orange_fraction,
        "centerX": selected["centerX"],
        "centerY": selected["centerY"],
        "radius": selected["radius"],
        "angularCoverage": selected["angularCoverage"],
        "annulusDensity": selected["annulusDensity"],
        "edgeVotes": selected["edgeVotes"],
        "votes": selected["votes"],
    }

def close_response(response, width, height):
    dilated = []
    for y in range(height):
        for x in range(width):
            maximum = 0
            for neighbor_y in range(max(0, y - 1), min(height, y + 2)):
                for neighbor_x in range(max(0, x - 1), min(width, x + 2)):
                    maximum = max(maximum, response[neighbor_y * width + neighbor_x])
            dilated.append(maximum)
    closed = []
    for y in range(height):
        for x in range(width):
            minimum = 255
            for neighbor_y in range(y - 1, y + 2):
                for neighbor_x in range(x - 1, x + 2):
                    value = 0
                    if neighbor_x >= 0 and neighbor_x < width and neighbor_y >= 0 and neighbor_y < height:
                        value = dilated[neighbor_y * width + neighbor_x]
                    minimum = min(minimum, value)
            closed.append(minimum)
    return closed

def component_candidates(response, thresholds, branch, width, height, scale, compass_center_x, compass_center_y, compass_radius):
    candidates = []
    processed_response = close_response(response, width, height) if scale >= 1.5 else response
    center_limit = compass_radius + 8.0 * scale
    for threshold in thresholds:
        binary = []
        for value in processed_response:
            binary.append(value >= threshold)
        visited = []
        for _ in range(width * height):
            visited.append(False)
        for start in range(width * height):
            if visited[start] or not binary[start]:
                continue
            visited[start] = True
            queue = [start]
            head = 0
            minimum_x = width
            minimum_y = height
            maximum_x = -1
            maximum_y = -1
            x_total = 0
            y_total = 0
            response_total = 0
            max_response = 0
            perimeter = 0
            for _ in range(width * height):
                if head >= len(queue):
                    break
                index = queue[head]
                head += 1
                x = index % width
                y = index // width
                minimum_x = min(minimum_x, x)
                minimum_y = min(minimum_y, y)
                maximum_x = max(maximum_x, x)
                maximum_y = max(maximum_y, y)
                x_total += x
                y_total += y
                response_total += processed_response[index]
                max_response = max(max_response, processed_response[index])
                for delta_y in [-1, 0, 1]:
                    for delta_x in [-1, 0, 1]:
                        if delta_x == 0 and delta_y == 0:
                            continue
                        neighbor_x = x + delta_x
                        neighbor_y = y + delta_y
                        if neighbor_x >= 0 and neighbor_x < width and neighbor_y >= 0 and neighbor_y < height:
                            neighbor = neighbor_y * width + neighbor_x
                            if binary[neighbor] and not visited[neighbor]:
                                visited[neighbor] = True
                                queue.append(neighbor)
                for side in [[-1, 0], [1, 0], [0, -1], [0, 1]]:
                    neighbor_x = x + side[0]
                    neighbor_y = y + side[1]
                    if neighbor_x < 0 or neighbor_x >= width or neighbor_y < 0 or neighbor_y >= height or not binary[neighbor_y * width + neighbor_x]:
                        perimeter += 1

            area = len(queue)
            component_width = maximum_x - minimum_x + 1
            component_height = maximum_y - minimum_y + 1
            reference_width = component_width / scale
            reference_height = component_height / scale
            if area < 4 or reference_width < 2.0 or reference_height < 2.0:
                continue
            if reference_width > 18.0 or reference_height > 18.0:
                continue
            aspect = reference_width / reference_height
            if aspect < 0.35 or aspect > 2.85:
                continue
            center_x = x_total / float(area)
            center_y = y_total / float(area)
            if math.hypot(center_x - compass_center_x, center_y - compass_center_y) > center_limit:
                continue
            fill = area / float(component_width * component_height)
            short_size = min(reference_width, reference_height)
            aspect_quality = math.exp(-abs(math.log(aspect)) / 0.45)
            solid_size_quality = max(0.0, 1.0 - abs(short_size - 8.0) / 6.0)
            hollow_size_quality = max(0.0, 1.0 - abs(short_size - 6.0) / 6.0)
            topology_x = minimum_x + (component_width - 1) / 2.0
            topology_y = minimum_y + (component_height - 1) / 2.0
            core_radius = max(1.0, min(component_width, component_height) * 0.18)
            core_count = 0
            core_area = 0
            for core_y in range(max(0, int(topology_y - core_radius)), min(height, int(topology_y + core_radius) + 1)):
                for core_x in range(max(0, int(topology_x - core_radius)), min(width, int(topology_x + core_radius) + 1)):
                    if math.hypot(core_x - topology_x, core_y - topology_y) <= core_radius:
                        core_area += 1
                        if binary[core_y * width + core_x]:
                            core_count += 1
            core_density = core_count / float(max(1, core_area))
            circularity = min(1.0, 4.0 * math.pi * area / float(max(1, perimeter * perimeter)))
            candidates.append({
                "branch": branch,
                "threshold": threshold,
                "x": minimum_x,
                "y": minimum_y,
                "w": component_width,
                "h": component_height,
                "centerX": center_x,
                "centerY": center_y,
                "area": area,
                "referenceWidth": reference_width,
                "referenceHeight": reference_height,
                "fill": fill,
                "aspectQuality": aspect_quality,
                "solidSizeQuality": solid_size_quality,
                "hollowSizeQuality": hollow_size_quality,
                "coreDensity": core_density,
                "circularity": circularity,
                "meanResponse": response_total / float(area),
                "maxResponse": max_response,
            })
    return candidates

def candidate_solid_score(candidate):
    return (
        2.2 * candidate["coreDensity"] +
        0.7 * candidate["fill"] +
        0.5 * candidate["circularity"] +
        1.3 * candidate["solidSizeQuality"] +
        0.8 * candidate["aspectQuality"] +
        0.012 * candidate["maxResponse"]
    )

def candidate_hollow_score(candidate):
    return (
        1.7 * (1.0 - candidate["coreDensity"]) +
        0.8 * candidate["circularity"] +
        0.45 * (1.0 - abs(candidate["fill"] - 0.35)) +
        0.8 * candidate["hollowSizeQuality"] +
        0.8 * candidate["aspectQuality"] +
        0.010 * candidate["maxResponse"]
    )

def route_features(candidates, scale):
    clusters = []
    for candidate in candidates:
        selected_cluster = None
        selected_distance = 1000000.0
        for cluster in clusters:
            center_x = cluster["xTotal"] / float(len(cluster["items"]))
            center_y = cluster["yTotal"] / float(len(cluster["items"]))
            distance = math.hypot(candidate["centerX"] - center_x, candidate["centerY"] - center_y)
            if distance <= 3.0 * scale and distance < selected_distance:
                selected_cluster = cluster
                selected_distance = distance
        if selected_cluster == None:
            clusters.append({"items": [candidate], "xTotal": candidate["centerX"], "yTotal": candidate["centerY"]})
        else:
            selected_cluster["items"].append(candidate)
            selected_cluster["xTotal"] += candidate["centerX"]
            selected_cluster["yTotal"] += candidate["centerY"]

    summaries = []
    for cluster in clusters:
        best_solid = None
        best_solid_score = -1000000.0
        best_hollow = None
        best_hollow_score = -1000000.0
        thresholds = {}
        for candidate in cluster["items"]:
            thresholds[str(candidate["threshold"])] = True
            solid_score = candidate_solid_score(candidate)
            hollow_score = candidate_hollow_score(candidate)
            if solid_score > best_solid_score:
                best_solid = candidate
                best_solid_score = solid_score
            if hollow_score > best_hollow_score:
                best_hollow = candidate
                best_hollow_score = hollow_score
        support = len(thresholds)
        support_bonus = 0.18 * math.log(1.0 + support) + 0.12
        hollow_size = min(best_hollow["referenceWidth"], best_hollow["referenceHeight"])
        hollow_size_bonus = max(0.0, 1.0 - abs(hollow_size - 7.0) / 7.0)
        summaries.append({
            "centerX": cluster["xTotal"] / float(len(cluster["items"])),
            "centerY": cluster["yTotal"] / float(len(cluster["items"])),
            "support": support,
            "solidScore": best_solid_score + support_bonus,
            "hollowScore": best_hollow_score + 0.45 * hollow_size_bonus + support_bonus,
            "bestSolid": best_solid,
            "bestHollow": best_hollow,
        })

    best_solid = None
    best_hollow = None
    for summary in summaries:
        if best_solid == None or summary["solidScore"] > best_solid["solidScore"]:
            best_solid = summary
        if best_hollow == None or summary["hollowScore"] > best_hollow["hollowScore"]:
            best_hollow = summary
    return {
        "solidScore": best_solid["solidScore"] if best_solid != None else 0.0,
        "hollowScore": best_hollow["hollowScore"] if best_hollow != None else 0.0,
        "solid": best_solid,
        "hollow": best_hollow,
        "clusterCount": len(summaries),
    }

def classify_route(features, solid_threshold, hollow_threshold, margin):
    solid_excess = features["solidScore"] - solid_threshold
    hollow_excess = features["hollowScore"] - hollow_threshold
    if solid_excess < 0 and hollow_excess < 0:
        prediction = "NONE"
        confidence = max(0.0, min(-solid_excess, -hollow_excess))
        selected = None
    elif solid_excess >= hollow_excess + margin:
        prediction = "SOLID"
        confidence = max(0.0, min(solid_excess, solid_excess - hollow_excess - margin))
        selected = features["solid"]
    else:
        prediction = "HOLLOW"
        confidence = max(0.0, min(hollow_excess, hollow_excess - solid_excess + margin))
        selected = features["hollow"]
    return {
        "prediction": prediction,
        "confidence": confidence,
        "solidScore": features["solidScore"],
        "hollowScore": features["hollowScore"],
        "clusterCount": features["clusterCount"],
        "selected": selected,
    }

def evaluate_route(response, thresholds, branch, width, height, scale, compass_center_x, compass_center_y, compass_radius, solid_threshold, hollow_threshold, margin):
    maximum = 0
    for value in response:
        maximum = max(maximum, value)
    if maximum < thresholds[0]:
        return classify_route({"solidScore": 0.0, "hollowScore": 0.0, "solid": None, "hollow": None, "clusterCount": 0}, solid_threshold, hollow_threshold, margin)
    candidates = component_candidates(response, thresholds, branch, width, height, scale, compass_center_x, compass_center_y, compass_radius)
    return classify_route(route_features(candidates, scale), solid_threshold, hollow_threshold, margin)

def arbitrate(strict_route, opponent_route):
    opponent_prediction = opponent_route["prediction"]
    strict_prediction = strict_route["prediction"]
    if opponent_prediction != "NONE" and opponent_route["confidence"] < CASCADE_MIN_CONFIDENCE:
        if strict_prediction != "NONE" and strict_route["confidence"] >= CASCADE_MIN_CONFIDENCE:
            return [strict_prediction, "STRICT_LOW_OPPONENT_CONFIDENCE", "STRICT", strict_route["confidence"], strict_route["selected"]]
        selected = opponent_route["selected"] if opponent_route["selected"] != None else strict_route["selected"]
        return ["UNKNOWN", "LOW_CONFIDENCE_DISAGREEMENT", "NONE", max(opponent_route["confidence"], strict_route["confidence"]), selected]
    if opponent_prediction != "NONE":
        if strict_prediction != "NONE" and strict_route["confidence"] >= CASCADE_MIN_CONFIDENCE and strict_prediction != opponent_prediction:
            return ["UNKNOWN", "CLASS_DISAGREEMENT", "NONE", min(opponent_route["confidence"], strict_route["confidence"]), opponent_route["selected"]]
        return [opponent_prediction, "OPPONENT_PRIMARY", "OPPONENT", opponent_route["confidence"], opponent_route["selected"]]
    if strict_prediction != "NONE" and strict_route["confidence"] >= CASCADE_MIN_CONFIDENCE:
        return [strict_prediction, "STRICT_RECOVERY", "STRICT", strict_route["confidence"], strict_route["selected"]]
    return ["NONE", "NO_MARKER", "NONE", max(opponent_route["confidence"], strict_route["confidence"]), None]

def route_output(route):
    return {
        "prediction": route["prediction"],
        "confidence": round_output(route["confidence"]),
        "solidScore": round_output(route["solidScore"]),
        "hollowScore": round_output(route["hollowScore"]),
        "clusterCount": route["clusterCount"],
    }

def empty_route(solid_threshold, hollow_threshold, margin):
    return classify_route({"solidScore": 0.0, "hollowScore": 0.0, "solid": None, "hollow": None, "clusterCount": 0}, solid_threshold, hollow_threshold, margin)

def analyze_sample(sample, expected_sampling):
    if sample["sampling"] != expected_sampling:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="Compass region sampling does not match the requested " + expected_sampling + " path")
    coordinate_space = sample["coordinateSpace"]
    if coordinate_space["width"] != REFERENCE_WIDTH or coordinate_space["height"] != REFERENCE_HEIGHT or coordinate_space["fit"] != "centered-16:9":
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="screen coordinate space is not the reviewed centered 1920x1080 reference")
    region = sample["region"]
    if (
        region["w"] != ROI_WIDTH or region["h"] != ROI_HEIGHT or
        region["x"] < WIDE_ROI_X or region["x"] > WIDE_ROI_X + WIDE_ROI_WIDTH - ROI_WIDTH or
        region["y"] < WIDE_ROI_Y or region["y"] > WIDE_ROI_Y + WIDE_ROI_HEIGHT - ROI_HEIGHT
    ):
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="screen region is outside the reviewed dynamic Compass window")
    image = sample["image"]
    image_width = image["width"]
    image_height = image["height"]
    if image["encoding"] != "rgb24-packed" or image_width <= 0 or image_height <= 0:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message=expected_sampling + " Compass image shape or encoding is invalid")
    if expected_sampling == "reference" and (image_width != ROI_WIDTH or image_height != ROI_HEIGHT):
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="reference Compass image is not 96x96")
    pixels = image["pixels"]
    if len(pixels) != image_width * image_height:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="screen region pixel count is incomplete")
    if image_width * image_height > 65536:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message=expected_sampling + " Compass image exceeds the declared pixel budget")

    scale_x = image_width / float(ROI_WIDTH)
    scale_y = image_height / float(ROI_HEIGHT)
    scale = (scale_x + scale_y) / 2.0
    responses = build_responses(pixels)
    reference_orange = native_mask_to_reference(responses["orange"], image_width, image_height)
    geometry = locate_compass_circle(reference_orange, ROI_WIDTH, ROI_HEIGHT, responses["orangeCount"], len(pixels), FAST_CENTER_MIN, FAST_CENTER_MAX_EXCLUSIVE)
    minimum_orange = int(MIN_ORANGE_PIXELS_REFERENCE * scale_x * scale_y)
    geometry_verified = responses["orangeCount"] >= minimum_orange and geometry["visible"]
    strict_route = empty_route(STRICT_SOLID_THRESHOLD, STRICT_HOLLOW_THRESHOLD, STRICT_MARGIN)
    opponent_route = empty_route(OPPONENT_SOLID_THRESHOLD, OPPONENT_HOLLOW_THRESHOLD, OPPONENT_MARGIN)
    if geometry_verified:
        compass_center_x = geometry["centerX"] * scale_x
        compass_center_y = geometry["centerY"] * scale_y
        compass_radius = geometry["radius"] * scale
        strict_route = evaluate_route(responses["strict"], STRICT_THRESHOLDS, "STRICT", image_width, image_height, scale, compass_center_x, compass_center_y, compass_radius, STRICT_SOLID_THRESHOLD, STRICT_HOLLOW_THRESHOLD, STRICT_MARGIN)
        opponent_route = evaluate_route(responses["opponent"], OPPONENT_THRESHOLDS, "OPPONENT", image_width, image_height, scale, compass_center_x, compass_center_y, compass_radius, OPPONENT_SOLID_THRESHOLD, OPPONENT_HOLLOW_THRESHOLD, OPPONENT_MARGIN)
    return {
        "sample": sample,
        "sampling": expected_sampling,
        "imageWidth": image_width,
        "imageHeight": image_height,
        "scaleX": scale_x,
        "scaleY": scale_y,
        "responses": responses,
        "geometry": geometry,
        "minimumOrange": minimum_orange,
        "geometryVerified": geometry_verified,
        "strictRoute": strict_route,
        "opponentRoute": opponent_route,
    }

def reference_escalation_reason(analysis):
    if not analysis["geometryVerified"]:
        return "REFERENCE_GEOMETRY_INSUFFICIENT"
    strict_route = analysis["strictRoute"]
    opponent_route = analysis["opponentRoute"]
    if strict_route["prediction"] == "NONE" or opponent_route["prediction"] == "NONE":
        return "REFERENCE_NO_MARKER"
    if strict_route["prediction"] != opponent_route["prediction"]:
        return "REFERENCE_CLASS_DISAGREEMENT"
    if strict_route["confidence"] < REFERENCE_FAST_PATH_MIN_CONFIDENCE or opponent_route["confidence"] < REFERENCE_FAST_PATH_MIN_CONFIDENCE:
        return "REFERENCE_LOW_CONFIDENCE"
    return None

def attempt_output(analysis, outcome, reason):
    return {
        "sampling": analysis["sampling"],
        "capturedAt": analysis["sample"]["frame"]["capturedAt"],
        "imageWidth": analysis["imageWidth"],
        "imageHeight": analysis["imageHeight"],
        "geometryVerified": analysis["geometryVerified"],
        "strictPrediction": analysis["strictRoute"]["prediction"],
        "strictConfidence": round_output(analysis["strictRoute"]["confidence"]),
        "opponentPrediction": analysis["opponentRoute"]["prediction"],
        "opponentConfidence": round_output(analysis["opponentRoute"]["confidence"]),
        "outcome": outcome,
        "reason": reason,
    }

def geometry_failure_summary(analysis):
    geometry = analysis["geometry"]
    return (
        "sampling=" + analysis["sampling"] +
        ",orange=" + str(analysis["responses"]["orangeCount"]) +
        ",minimum=" + str(analysis["minimumOrange"]) +
        ",candidates=" + str(geometry["circleCandidateCount"]) +
        ",orangeFraction=" + str(round_output(geometry["orangeFraction"])) +
        ",coverage=" + str(geometry["angularCoverage"]) +
        ",density=" + str(None if geometry["annulusDensity"] == None else round_output(geometry["annulusDensity"])) +
        ",edgeVotes=" + str(geometry["edgeVotes"]) +
        ",votes=" + str(geometry["votes"])
    )

def localization_output(path, sample, geometry):
    return {
        "path": path,
        "capturedAt": sample["frame"]["capturedAt"],
        "region": sample["region"],
        "circleCenterX": sample["region"]["x"] + geometry["centerX"],
        "circleCenterY": sample["region"]["y"] + geometry["centerY"],
        "circleRadiusPixels": geometry["radius"],
        "houghEdgeVotes": geometry["edgeVotes"],
        "houghVotes": geometry["votes"],
        "angularCoverageBins": geometry["angularCoverage"],
        "annulusDensity": round_output(geometry["annulusDensity"]),
        "orangeFraction": round_output(geometry["orangeFraction"]),
    }

def result_output(analysis, sampling_path, attempts, localization):
    sample = analysis["sample"]
    coordinate_space = sample["coordinateSpace"]
    image_width = analysis["imageWidth"]
    image_height = analysis["imageHeight"]
    scale_x = analysis["scaleX"]
    scale_y = analysis["scaleY"]
    geometry = analysis["geometry"]
    responses = analysis["responses"]
    minimum_orange = analysis["minimumOrange"]
    strict_route = analysis["strictRoute"]
    opponent_route = analysis["opponentRoute"]
    arbitration = arbitrate(strict_route, opponent_route)
    prediction = arbitration[0]
    cascade_mode = arbitration[1]
    selected_route = arbitration[2]
    classification_confidence = arbitration[3]
    selected = arbitration[4]

    target = {
        "detected": prediction != "NONE",
        "presentation": prediction if prediction in ["SOLID", "HOLLOW"] else "UNKNOWN",
        "hemisphere": "FRONT" if prediction == "SOLID" else ("REAR" if prediction == "HOLLOW" else "UNKNOWN"),
        "cascadeMode": cascade_mode,
        "selectedRoute": selected_route,
        "classificationConfidence": round_output(classification_confidence),
        "markerBounds": None,
        "referenceX": None,
        "referenceY": None,
        "offsetX": None,
        "offsetY": None,
        "screenAngleDegrees": None,
        "centerDistancePixels": None,
        "centerZone": {"shape": "circle", "radiusPixels": CENTER_ZONE_RADIUS, "inside": None},
    }
    if selected != None and prediction != "NONE":
        selected_component = selected["bestSolid"] if prediction == "SOLID" else selected["bestHollow"]
        region_x = sample["region"]["x"]
        region_y = sample["region"]["y"]
        reference_x = region_x + int(math.round(selected["centerX"] / scale_x))
        reference_y = region_y + int(math.round(selected["centerY"] / scale_y))
        compass_reference_x = region_x + geometry["centerX"]
        compass_reference_y = region_y + geometry["centerY"]
        offset_x = reference_x - compass_reference_x
        offset_y = reference_y - compass_reference_y
        distance = math.hypot(offset_x, offset_y)
        angle = None
        if distance != 0:
            angle = math.degrees(math.atan2(offset_x, -offset_y))
            if angle < 0:
                angle += 360
            angle = round_output(angle)
        marker_x = region_x + int(math.floor(selected_component["x"] / scale_x))
        marker_y = region_y + int(math.floor(selected_component["y"] / scale_y))
        marker_w = max(1, int(math.ceil(selected_component["w"] / scale_x)))
        marker_h = max(1, int(math.ceil(selected_component["h"] / scale_y)))
        target.update({
            "markerBounds": {"x": marker_x, "y": marker_y, "w": marker_w, "h": marker_h, "centerX": reference_x, "centerY": reference_y},
            "referenceX": reference_x,
            "referenceY": reference_y,
            "offsetX": offset_x,
            "offsetY": offset_y,
            "screenAngleDegrees": angle,
            "centerDistancePixels": round_output(distance),
            "centerZone": {"shape": "circle", "radiusPixels": CENTER_ZONE_RADIUS, "inside": distance <= CENTER_ZONE_RADIUS},
        })

    return {
        "schemaVersion": 8,
        "profile": {"width": sample["frame"]["width"], "height": sample["frame"]["height"], "capturedAt": sample["frame"]["capturedAt"]},
        "coordinateSpace": coordinate_space,
        "region": sample["region"],
        "physicalRegion": sample["physicalRegion"],
        "localization": localization,
        "samplingPath": sampling_path,
        "fallbackUsed": sampling_path != "REFERENCE_FAST_PATH",
        "attempts": attempts,
        "sampling": {"mode": analysis["sampling"], "imageWidth": image_width, "imageHeight": image_height, "scaleX": round_output(scale_x), "scaleY": round_output(scale_y)},
        "compass": {
            "visible": True,
            "orangePixelCount": responses["orangeCount"],
            "minimumOrangePixelCount": minimum_orange,
            "circleCenterX": sample["region"]["x"] + geometry["centerX"],
            "circleCenterY": sample["region"]["y"] + geometry["centerY"],
            "circleRadiusPixels": geometry["radius"],
            "circleCandidateCount": geometry["circleCandidateCount"],
            "houghPerimeterSamples": geometry["houghPerimeterSamples"],
            "houghEdgeVotes": geometry["edgeVotes"],
            "houghVotes": geometry["votes"],
            "angularCoverageBins": geometry["angularCoverage"],
            "annulusDensity": round_output(geometry["annulusDensity"]),
            "orangeFraction": round_output(geometry["orangeFraction"]),
        },
        "routes": {"strict": route_output(strict_route), "opponent": route_output(opponent_route)},
        "target": target,
    }

def localize_wide_reference(sample):
    if sample["sampling"] != "reference":
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="wide Compass localization did not use reference sampling")
    coordinate_space = sample["coordinateSpace"]
    if coordinate_space["width"] != REFERENCE_WIDTH or coordinate_space["height"] != REFERENCE_HEIGHT or coordinate_space["fit"] != "centered-16:9":
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="wide Compass coordinate space is not the reviewed centered 1920x1080 reference")
    region = sample["region"]
    if region["x"] != WIDE_ROI_X or region["y"] != WIDE_ROI_Y or region["w"] != WIDE_ROI_WIDTH or region["h"] != WIDE_ROI_HEIGHT:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="wide Compass region does not match the reviewed 192x192 localization window")
    image = sample["image"]
    if image["encoding"] != "rgb24-packed" or image["width"] != WIDE_ROI_WIDTH or image["height"] != WIDE_ROI_HEIGHT:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="wide Compass reference image is not 192x192 rgb24-packed")
    pixels = image["pixels"]
    if len(pixels) != WIDE_ROI_WIDTH * WIDE_ROI_HEIGHT:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="wide Compass reference pixel count is incomplete")
    responses = build_responses(pixels)
    geometry = locate_compass_circle(
        responses["orange"],
        WIDE_ROI_WIDTH,
        WIDE_ROI_HEIGHT,
        responses["orangeCount"],
        len(pixels),
        WIDE_CENTER_MIN,
        WIDE_CENTER_MAX_EXCLUSIVE,
    )
    if responses["orangeCount"] < MIN_ORANGE_PIXELS_REFERENCE or not geometry["visible"]:
        return job.fail(
            code="COMPASS_NOT_VISIBLE",
            message=(
                "wide 192x192 reference localization lacks verified Compass geometry: orange=" + str(responses["orangeCount"]) +
                ",candidates=" + str(geometry["circleCandidateCount"]) +
                ",fraction=" + str(round_output(geometry["orangeFraction"]))
            ),
        )
    return {"sample": sample, "geometry": geometry}

def localized_reference_sample(wide_sample, geometry):
    local_x = geometry["centerX"] - ROI_WIDTH // 2
    local_y = geometry["centerY"] - ROI_HEIGHT // 2
    if local_x < 0 or local_x + ROI_WIDTH > WIDE_ROI_WIDTH or local_y < 0 or local_y + ROI_HEIGHT > WIDE_ROI_HEIGHT:
        return job.fail(code="COMPASS_EVIDENCE_INVALID", message="wide Compass localization cannot produce a complete 96x96 reference crop")
    wide_pixels = wide_sample["image"]["pixels"]
    pixels = []
    for y in range(ROI_HEIGHT):
        source_y = local_y + y
        for x in range(ROI_WIDTH):
            pixels.append(wide_pixels[source_y * WIDE_ROI_WIDTH + local_x + x])
    physical = wide_sample["physicalRegion"]
    physical_scale_x = physical["width"] / float(WIDE_ROI_WIDTH)
    physical_scale_y = physical["height"] / float(WIDE_ROI_HEIGHT)
    return {
        "sampling": "reference",
        "coordinateSpace": wide_sample["coordinateSpace"],
        "frame": wide_sample["frame"],
        "viewport": wide_sample["viewport"],
        "region": {
            "x": WIDE_ROI_X + local_x,
            "y": WIDE_ROI_Y + local_y,
            "w": ROI_WIDTH,
            "h": ROI_HEIGHT,
        },
        "physicalRegion": {
            "left": physical["left"] + int(math.round(local_x * physical_scale_x)),
            "top": physical["top"] + int(math.round(local_y * physical_scale_y)),
            "width": int(math.round(ROI_WIDTH * physical_scale_x)),
            "height": int(math.round(ROI_HEIGHT * physical_scale_y)),
        },
        "image": {"width": ROI_WIDTH, "height": ROI_HEIGHT, "encoding": "rgb24-packed", "pixels": pixels},
    }

def main(ctx):
    sample = observer.screen.read_region(
        x = ROI_X,
        y = ROI_Y,
        w = ROI_WIDTH,
        h = ROI_HEIGHT,
        sampling = "reference",
    )
    reference_analysis = analyze_sample(sample, "reference")
    escalation_reason = reference_escalation_reason(reference_analysis)
    if escalation_reason == None:
        return result_output(
            reference_analysis,
            "REFERENCE_FAST_PATH",
            [attempt_output(reference_analysis, "ACCEPTED", "REFERENCE_DUAL_ROUTE_CONFIRMED")],
            localization_output("FIXED_96", sample, reference_analysis["geometry"]),
        )

    attempts = [attempt_output(reference_analysis, "ESCALATED", escalation_reason)]
    wide_sample = observer.screen.read_region(
        x = WIDE_ROI_X,
        y = WIDE_ROI_Y,
        w = WIDE_ROI_WIDTH,
        h = WIDE_ROI_HEIGHT,
        sampling = "reference",
    )
    wide = localize_wide_reference(wide_sample)
    localized_sample = localized_reference_sample(wide_sample, wide["geometry"])
    localized_analysis = analyze_sample(localized_sample, "reference")
    if not localized_analysis["geometryVerified"]:
        return job.fail(
            code="COMPASS_NOT_VISIBLE",
            message=(
                "reference path escalated with " + escalation_reason +
                " (" + geometry_failure_summary(reference_analysis) + "); " +
                "wide localization found center " + str(wide["geometry"]["centerX"]) + "," + str(wide["geometry"]["centerY"]) +
                "; localized reference Compass ROI lacks verified orange annular geometry (" + geometry_failure_summary(localized_analysis) + ")"
            ),
        )
    attempts.append(attempt_output(localized_analysis, "ACCEPTED", "WIDE_192_LOCALIZED_REFERENCE_FALLBACK_COMPLETED"))
    return result_output(
        localized_analysis,
        "WIDE_REFERENCE_FALLBACK",
        attempts,
        localization_output("WIDE_192", wide_sample, wide["geometry"]),
    )
