WIDTH = 256
HEIGHT = 144
PIXEL_COUNT = WIDTH * HEIGHT
REFERENCE_SCALE = 7.5
LIBRARY = "sphere-cv"
OK = 0

SPHERE_RESULT = native.struct(fields = [
    {"name": "state", "type": native.u32()},
    {"name": "control", "type": native.u32()},
    {"name": "centerXMilli", "type": native.i32()},
    {"name": "centerYMilli", "type": native.i32()},
    {"name": "radiusMilli", "type": native.u32()},
    {"name": "signedClearanceMilli", "type": native.i32()},
    {"name": "confidencePermille", "type": native.u32()},
    {"name": "occupiedAngularBins", "type": native.u32()},
    {"name": "inlierCount", "type": native.u32()},
    {"name": "boundaryPointCount", "type": native.u32()},
    {"name": "medianResidualMilli", "type": native.u32()},
    {"name": "otsuThreshold", "type": native.u32()},
    {"name": "blackPermille", "type": native.u32()},
    {"name": "whitePermille", "type": native.u32()},
    {"name": "candidateCount", "type": native.u32()},
])

CONTROLS = {
    1: "PITCH_UP", 2: "PITCH_DOWN", 3: "YAW_LEFT", 4: "YAW_RIGHT",
    5: "PITCH_UP_YAW_LEFT", 6: "PITCH_UP_YAW_RIGHT",
    7: "PITCH_DOWN_YAW_LEFT", 8: "PITCH_DOWN_YAW_RIGHT",
}

def main(ctx):
    sample = observer.screen.read_region(x=0, y=0, w=WIDTH, h=HEIGHT, sampling="reference")
    library = native.load_library(LIBRARY)
    analyze = library.bind(
        name="elite_supercruise_sphere_analyze",
        parameters=[native.array(native.u32(), PIXEL_COUNT), native.u32(), native.u32(), native.out(SPHERE_RESULT)],
        result=native.i32(),
    )
    called = analyze.call(sample["image"]["pixels"], WIDTH, HEIGHT)
    if called["result"] != OK:
        return job.fail(code="SPHERE_CV_FAILED", message="sphere CV rejected the current reference frame")
    result = called["out"][0]
    detected = result["state"] == 1
    state = "DETECTED" if detected else ("ABSENT" if result["state"] == 2 else "UNKNOWN")
    control = CONTROLS.get(result["control"])
    return {
        "schemaVersion": 1,
        "sphere": {
            "state": state,
            "centerX": float(result["centerXMilli"]) * REFERENCE_SCALE / 1000.0 if detected else None,
            "centerY": float(result["centerYMilli"]) * REFERENCE_SCALE / 1000.0 if detected else None,
            "radiusPixels": float(result["radiusMilli"]) * REFERENCE_SCALE / 1000.0 if detected else None,
            "signedLimbClearancePixels": float(result["signedClearanceMilli"]) * REFERENCE_SCALE / 1000.0 if detected else None,
            "confidencePermille": result["confidencePermille"],
            "occupiedAngularBins": result["occupiedAngularBins"],
            "inlierCount": result["inlierCount"],
            "boundaryPointCount": result["boundaryPointCount"],
            "medianResidualPixels": float(result["medianResidualMilli"]) * REFERENCE_SCALE / 1000.0 if detected else None,
            "candidateCount": result["candidateCount"],
            "reason": "CURRENT_FRAME_SPHERE_CONFIRMED" if detected else ("CURRENT_FRAME_NO_SPHERE_CANDIDATE" if state == "ABSENT" else "CURRENT_FRAME_SPHERE_AMBIGUOUS"),
        },
        "direction": {
            "state": "READY" if detected and control != None else "UNKNOWN",
            "control": control,
            "reason": "MOVE_AWAY_FROM_CURRENT_SPHERE_CENTER" if detected and control != None else "SPHERE_DIRECTION_NOT_AVAILABLE",
        },
        "evidence": {
            "sampling": "reference", "width": WIDTH, "height": HEIGHT,
            "capturedAt": sample["frame"]["capturedAt"],
            "algorithm": "GAUSSIAN_9X9_OTSU_ROBUST_CIRCLE_REFERENCE_256X144",
            "otsuThreshold": result["otsuThreshold"],
            "blackPermille": result["blackPermille"], "whitePermille": result["whitePermille"],
        },
    }
