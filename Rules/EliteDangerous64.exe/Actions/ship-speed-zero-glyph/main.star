REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
ROI_X = 1100
ROI_Y = 815
ROI_WIDTH = 65
ROI_HEIGHT = 50

SEARCH_MIN_X = 8
SEARCH_MAX_X = 56
SEARCH_MIN_Y = 4
SEARCH_MAX_Y = 34
MIN_COMPONENT_AREA = 30
MIN_COMPONENT_WIDTH = 4
MAX_COMPONENT_WIDTH = 14
MIN_COMPONENT_HEIGHT = 10
MAX_COMPONENT_HEIGHT = 22
ZERO_MIN_AREA = 90
ZERO_MIN_WIDTH = 9
ZERO_MAX_WIDTH = 13
ZERO_MIN_HEIGHT = 15
ZERO_MAX_HEIGHT = 20
ZERO_MIN_HOLES = 2
ZERO_MIN_HOLE_AREA = 12
ZERO_MAX_HOLE_AREA = 70

def channels(pixel):
    return pixel // 65536, (pixel // 256) % 256, pixel % 256

def is_orange(pixel):
    red, green, blue = channels(pixel)
    return red >= 140 and green >= 35 and green <= 210 and blue <= 80 and red >= green + 25

def inside_search(x, y):
    return x >= SEARCH_MIN_X and x < SEARCH_MAX_X and y >= SEARCH_MIN_Y and y < SEARCH_MAX_Y

def component_from(seed, mask, visited):
    queue = [seed]
    visited[seed] = True
    queue_index = 0
    min_x = seed % ROI_WIDTH
    max_x = min_x
    min_y = seed // ROI_WIDTH
    max_y = min_y
    area = 0
    for unused in range(ROI_WIDTH * ROI_HEIGHT):
        if queue_index >= len(queue):
            break
        index = queue[queue_index]
        queue_index += 1
        x = index % ROI_WIDTH
        y = index // ROI_WIDTH
        area += 1
        min_x = min(min_x, x)
        max_x = max(max_x, x)
        min_y = min(min_y, y)
        max_y = max(max_y, y)
        for neighbor_y in range(max(SEARCH_MIN_Y, y - 1), min(SEARCH_MAX_Y, y + 2)):
            for neighbor_x in range(max(SEARCH_MIN_X, x - 1), min(SEARCH_MAX_X, x + 2)):
                neighbor = neighbor_y * ROI_WIDTH + neighbor_x
                if mask[neighbor] and not visited[neighbor]:
                    visited[neighbor] = True
                    queue.append(neighbor)
    return {"minX": min_x, "minY": min_y, "maxX": max_x, "maxY": max_y, "width": max_x - min_x + 1, "height": max_y - min_y + 1, "area": area}

def enclosed_background(mask, component):
    min_x = max(SEARCH_MIN_X, component["minX"] - 1)
    max_x = min(SEARCH_MAX_X - 1, component["maxX"] + 1)
    min_y = max(SEARCH_MIN_Y, component["minY"] - 1)
    max_y = min(SEARCH_MAX_Y - 1, component["maxY"] + 1)
    outside = [False] * (ROI_WIDTH * ROI_HEIGHT)
    queue = []
    for x in range(min_x, max_x + 1):
        for y in [min_y, max_y]:
            index = y * ROI_WIDTH + x
            if not mask[index] and not outside[index]:
                outside[index] = True
                queue.append(index)
    for y in range(min_y, max_y + 1):
        for x in [min_x, max_x]:
            index = y * ROI_WIDTH + x
            if not mask[index] and not outside[index]:
                outside[index] = True
                queue.append(index)
    queue_index = 0
    for unused in range(ROI_WIDTH * ROI_HEIGHT):
        if queue_index >= len(queue):
            break
        index = queue[queue_index]
        queue_index += 1
        x = index % ROI_WIDTH
        y = index // ROI_WIDTH
        for delta in [[1, 0], [-1, 0], [0, 1], [0, -1]]:
            neighbor_x = x + delta[0]
            neighbor_y = y + delta[1]
            if neighbor_x < min_x or neighbor_x > max_x or neighbor_y < min_y or neighbor_y > max_y:
                continue
            neighbor = neighbor_y * ROI_WIDTH + neighbor_x
            if not mask[neighbor] and not outside[neighbor]:
                outside[neighbor] = True
                queue.append(neighbor)

    hole_visited = [False] * (ROI_WIDTH * ROI_HEIGHT)
    holes = 0
    hole_area = 0
    for y in range(min_y, max_y + 1):
        for x in range(min_x, max_x + 1):
            seed = y * ROI_WIDTH + x
            if mask[seed] or outside[seed] or hole_visited[seed]:
                continue
            holes += 1
            queue = [seed]
            hole_visited[seed] = True
            queue_index = 0
            for unused in range(ROI_WIDTH * ROI_HEIGHT):
                if queue_index >= len(queue):
                    break
                index = queue[queue_index]
                queue_index += 1
                hole_area += 1
                current_x = index % ROI_WIDTH
                current_y = index // ROI_WIDTH
                for delta in [[1, 0], [-1, 0], [0, 1], [0, -1]]:
                    neighbor_x = current_x + delta[0]
                    neighbor_y = current_y + delta[1]
                    if neighbor_x < min_x or neighbor_x > max_x or neighbor_y < min_y or neighbor_y > max_y:
                        continue
                    neighbor = neighbor_y * ROI_WIDTH + neighbor_x
                    if not mask[neighbor] and not outside[neighbor] and not hole_visited[neighbor]:
                        hole_visited[neighbor] = True
                        queue.append(neighbor)
    return holes, hole_area

def main(ctx):
    sample = observer.screen.read_region(x=ROI_X, y=ROI_Y, w=ROI_WIDTH, h=ROI_HEIGHT, sampling="reference")
    if sample["sampling"] != "reference":
        return job.fail(code="SHIP_SPEED_GLYPH_EVIDENCE_INVALID", message="screen region sampling is not reference")
    coordinate_space = sample["coordinateSpace"]
    if coordinate_space["width"] != REFERENCE_WIDTH or coordinate_space["height"] != REFERENCE_HEIGHT or coordinate_space["fit"] != "centered-16:9":
        return job.fail(code="SHIP_SPEED_GLYPH_EVIDENCE_INVALID", message="screen coordinate space is not the reviewed reference")
    region = sample["region"]
    if region["x"] != ROI_X or region["y"] != ROI_Y or region["w"] != ROI_WIDTH or region["h"] != ROI_HEIGHT:
        return job.fail(code="SHIP_SPEED_GLYPH_EVIDENCE_INVALID", message="screen region does not match the reviewed coordinates")
    image = sample["image"]
    pixels = image["pixels"]
    if image["width"] != ROI_WIDTH or image["height"] != ROI_HEIGHT or image["encoding"] != "rgb24-packed" or len(pixels) != ROI_WIDTH * ROI_HEIGHT:
        return job.fail(code="SHIP_SPEED_GLYPH_EVIDENCE_INVALID", message="screen region pixels are incomplete")

    mask = []
    orange_count = 0
    for index in range(len(pixels)):
        x = index % ROI_WIDTH
        y = index // ROI_WIDTH
        selected = inside_search(x, y) and is_orange(pixels[index])
        mask.append(selected)
        if selected:
            orange_count += 1

    visited = [False] * len(mask)
    candidates = []
    for index in range(len(mask)):
        if not mask[index] or visited[index]:
            continue
        component = component_from(index, mask, visited)
        if component["area"] >= MIN_COMPONENT_AREA and component["width"] >= MIN_COMPONENT_WIDTH and component["width"] <= MAX_COMPONENT_WIDTH and component["height"] >= MIN_COMPONENT_HEIGHT and component["height"] <= MAX_COMPONENT_HEIGHT:
            candidates.append(component)

    state = "UNKNOWN"
    reason = "GLYPH_COMPONENT_MISSING"
    selected = None
    if len(candidates) > 1:
        state = "NOT_ZERO"
        reason = "MULTI_DIGIT_COMPONENTS_OBSERVED"
    elif len(candidates) == 1:
        selected = candidates[0]
        holes, hole_area = enclosed_background(mask, selected)
        selected["holeCount"] = holes
        selected["holeArea"] = hole_area
        if selected["area"] >= ZERO_MIN_AREA and selected["width"] >= ZERO_MIN_WIDTH and selected["width"] <= ZERO_MAX_WIDTH and selected["height"] >= ZERO_MIN_HEIGHT and selected["height"] <= ZERO_MAX_HEIGHT and holes >= ZERO_MIN_HOLES and hole_area >= ZERO_MIN_HOLE_AREA and hole_area <= ZERO_MAX_HOLE_AREA:
            state = "ZERO"
            reason = "SLASHED_ZERO_TOPOLOGY_CONFIRMED"
        else:
            state = "NOT_ZERO"
            reason = "SINGLE_NONZERO_GLYPH_OBSERVED"

    return {
        "schemaVersion": 1,
        "profile": {"width": sample["frame"]["width"], "height": sample["frame"]["height"], "capturedAt": sample["frame"]["capturedAt"]},
        "coordinateSpace": coordinate_space,
        "region": region,
        "physicalRegion": sample["physicalRegion"],
        "zeroGlyph": {
            "state": state,
            "reason": reason,
            "candidateCount": len(candidates),
            "orangePixelCount": orange_count,
            "component": selected,
            "thresholds": {"minimumArea": ZERO_MIN_AREA, "width": [ZERO_MIN_WIDTH, ZERO_MAX_WIDTH], "height": [ZERO_MIN_HEIGHT, ZERO_MAX_HEIGHT], "minimumHoles": ZERO_MIN_HOLES, "holeArea": [ZERO_MIN_HOLE_AREA, ZERO_MAX_HOLE_AREA]},
        },
    }
