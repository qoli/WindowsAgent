REFERENCE_WIDTH = 1920
REFERENCE_HEIGHT = 1080
ROI_X = 120
ROI_Y = 80
ROI_WIDTH = 1680
ROI_HEIGHT = 900
STRIP_HEIGHT = 7
STRIP_Y = [20, 160, 300, 440, 580]
SAMPLED_CENTER_Y = 230.0
GRID_COLUMNS = 5
GRID_ROWS = 5
CLEAR_RATIO = 0.015
BLOCKING_CENTER_RATIO = 0.25
BLOCKING_TOTAL_RATIO = 0.12
SAFE_CHARGE_TOTAL_RATIO = 0.005
SAFE_CHARGE_MAX_CELL_RATIO = 0.02
MIN_DIRECTION_MAGNITUDE = 0.08

def rounded(value):
    return math.round(value * 1000000.0) / 1000000.0

def is_bright(red, green, blue):
    luminance = (2126 * red + 7152 * green + 722 * blue) // 10000
    return luminance >= 180

def is_warm_orange(red, green, blue):
    return red >= 150 and green >= 55 and red >= blue + 25 and green >= blue + 10

def main(ctx):
    cell_width = ROI_WIDTH // GRID_COLUMNS
    grid_ratios = []
    bright_count = 0
    warm_count = 0
    stellar_count = 0
    sampled_count = 0
    stellar_x_total = 0
    stellar_y_total = 0

    sample = None
    for strip_index in range(GRID_ROWS):
        strip_y = STRIP_Y[strip_index]
        sample = observer.screen.read_region(x=ROI_X, y=strip_y, w=ROI_WIDTH, h=STRIP_HEIGHT, sampling="reference")
        image = sample["image"]
        if sample["sampling"] != "reference" or sample["coordinateSpace"]["width"] != REFERENCE_WIDTH or sample["coordinateSpace"]["height"] != REFERENCE_HEIGHT or image["encoding"] != "rgb24-packed" or len(image["pixels"]) != ROI_WIDTH * STRIP_HEIGHT:
            return job.fail(code="HYPERSPACE_OCCLUSION_EVIDENCE_INVALID", message="wide-field forward-view strip is incomplete")
        pixels = image["pixels"]
        for grid_x in range(GRID_COLUMNS):
            cell_stellar = 0
            cell_samples = 0
            start_x = grid_x * cell_width
            end_x = start_x + cell_width
            for pixel_y in range(STRIP_HEIGHT):
                for pixel_x in range(start_x, end_x):
                    pixel = pixels[pixel_y * ROI_WIDTH + pixel_x]
                    red = pixel // 65536
                    green = (pixel // 256) % 256
                    blue = pixel % 256
                    bright = is_bright(red, green, blue)
                    warm = is_warm_orange(red, green, blue)
                    # HUD and cockpit trim contain warm orange by design.
                    # Only high-luminance pixels own the wide-field stellar
                    # coverage and centroid; warm evidence remains diagnostic.
                    stellar = bright
                    sampled_count += 1
                    cell_samples += 1
                    if bright:
                        bright_count += 1
                    if warm:
                        warm_count += 1
                    if stellar:
                        stellar_count += 1
                        cell_stellar += 1
                        stellar_x_total += pixel_x
                        stellar_y_total += strip_y - ROI_Y + pixel_y
            grid_ratios.append(rounded(float(cell_stellar) / float(cell_samples)))

    bright_ratio = rounded(float(bright_count) / float(sampled_count))
    warm_ratio = rounded(float(warm_count) / float(sampled_count))
    stellar_ratio = rounded(float(stellar_count) / float(sampled_count))
    center_ratio = grid_ratios[12]
    max_cell_ratio = 0.0
    for cell_ratio in grid_ratios:
        max_cell_ratio = max(max_cell_ratio, cell_ratio)
    safe_to_charge = stellar_ratio <= SAFE_CHARGE_TOTAL_RATIO and max_cell_ratio <= SAFE_CHARGE_MAX_CELL_RATIO
    state = "CLEAR"
    if center_ratio >= BLOCKING_CENTER_RATIO or stellar_ratio >= BLOCKING_TOTAL_RATIO:
        state = "BLOCKING"
    elif center_ratio >= CLEAR_RATIO or stellar_ratio >= CLEAR_RATIO:
        state = "PARTIAL"

    centroid_x = None
    centroid_y = None
    escape_x = None
    escape_y = None
    direction_confidence = 0.0
    recommended_control = None
    if stellar_count > 0:
        centroid_x = rounded(float(stellar_x_total) / float(stellar_count))
        # The top sampling strip starts above ROI_Y so it can cover the
        # canopy-edge blind spot. Keep the public centroid ROI-relative and
        # schema-valid while retaining the top-edge direction in escape_y.
        raw_centroid_y = rounded(float(stellar_y_total) / float(stellar_count))
        centroid_y = max(0.0, min(float(ROI_HEIGHT - 1), raw_centroid_y))
        escape_x = rounded((centroid_x - float(ROI_WIDTH) / 2.0) / (float(ROI_WIDTH) / 2.0))
        # The sparse strips intentionally exclude the lower cockpit. Their
        # vertical mean, not the full ROI midpoint, is the neutral centroid.
        escape_y = rounded((raw_centroid_y - SAMPLED_CENTER_Y) / (float(ROI_HEIGHT) / 2.0))
        magnitude = math.sqrt(escape_x * escape_x + escape_y * escape_y)
        direction_confidence = rounded(min(1.0, magnitude / 0.5))
        if magnitude >= MIN_DIRECTION_MAGNITUDE:
            if abs(escape_y) >= abs(escape_x):
                recommended_control = "PITCH_UP" if escape_y > 0 else "PITCH_DOWN"
            else:
                recommended_control = "YAW_LEFT" if escape_x > 0 else "YAW_RIGHT"

    return {
        "schemaVersion": 3,
        "occlusion": {
            "state": state,
            "brightPixelCount": bright_count,
            "warmOrangePixelCount": warm_count,
            "stellarPixelCount": stellar_count,
            "sampledPixelCount": sampled_count,
            "brightRatio": bright_ratio,
            "warmOrangeRatio": warm_ratio,
            "stellarCoverageRatio": stellar_ratio,
            "centerCoverageRatio": center_ratio,
            "maximumCellCoverageRatio": max_cell_ratio,
            "safeToCharge": safe_to_charge,
            "safeChargeTotalRatio": SAFE_CHARGE_TOTAL_RATIO,
            "safeChargeMaximumCellRatio": SAFE_CHARGE_MAX_CELL_RATIO,
            "clearRatio": CLEAR_RATIO,
            "blockingCenterRatio": BLOCKING_CENTER_RATIO,
            "blockingTotalRatio": BLOCKING_TOTAL_RATIO,
            "gridColumns": GRID_COLUMNS,
            "gridRows": GRID_ROWS,
            "gridCoverageRatios": grid_ratios,
            "centroidX": centroid_x,
            "centroidY": centroid_y,
            "escapeVectorX": escape_x,
            "escapeVectorY": escape_y,
            "directionConfidence": direction_confidence,
            "recommendedControl": recommended_control,
        },
        "profile": {"width": sample["frame"]["width"], "height": sample["frame"]["height"], "capturedAt": sample["frame"]["capturedAt"]},
        "region": {"x": ROI_X, "y": ROI_Y, "w": ROI_WIDTH, "h": ROI_HEIGHT, "stripHeight": STRIP_HEIGHT, "stripY": STRIP_Y},
    }
