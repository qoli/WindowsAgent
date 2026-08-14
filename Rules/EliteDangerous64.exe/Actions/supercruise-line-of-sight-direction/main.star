MIN_DIRECTION_OFFSET_PIXELS = 48.0
DIAGONAL_COMPONENT_MIN_PIXELS = 32.0
DIAGONAL_COMPONENT_RATIO_LIMIT = 3.0
MIN_SHAPE_CONFIDENCE_PERMILLE = 520
MIN_FOCUS_FRAME_CONFIDENCE_PERMILLE = 650

def unknown(target, reason):
    return {
        "schemaVersion": 1,
        "direction": {
            "state": "UNKNOWN",
            "control": None,
            "reason": reason,
            "targetPresentation": target["presentation"],
            "targetOffsetX": target["offsetX"],
            "targetOffsetY": target["offsetY"],
            "targetCenterDistancePixels": target["centerDistancePixels"],
            "initialProjectionPixels": None,
            "reticleEvidencePlane": target["reticleEvidencePlane"],
            "shapeConfidencePermille": target["shapeConfidencePermille"],
            "focusFrameConfidencePermille": target["focusFrameConfidencePermille"],
            "identityConfirmed": target["identityConfirmed"],
            "reticleCapturedAt": target["reticleCapturedAt"],
        },
    }

def choose_control(offset_x, offset_y):
    absolute_x = abs(offset_x)
    absolute_y = abs(offset_y)
    if (
        absolute_x >= DIAGONAL_COMPONENT_MIN_PIXELS and
        absolute_y >= DIAGONAL_COMPONENT_MIN_PIXELS and
        absolute_x <= absolute_y * DIAGONAL_COMPONENT_RATIO_LIMIT and
        absolute_y <= absolute_x * DIAGONAL_COMPONENT_RATIO_LIMIT
    ):
        pitch = "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
        yaw = "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
        return pitch + "_" + yaw
    if absolute_x >= absolute_y and offset_x != 0:
        return "YAW_RIGHT" if offset_x > 0 else "YAW_LEFT"
    if offset_y != 0:
        return "PITCH_DOWN" if offset_y > 0 else "PITCH_UP"
    return None

def initial_projection(control, offset_x, offset_y):
    if control == "YAW_RIGHT":
        return offset_x
    if control == "YAW_LEFT":
        return -offset_x
    if control == "PITCH_DOWN":
        return offset_y
    if control == "PITCH_UP":
        return -offset_y
    horizontal = offset_x if "YAW_RIGHT" in control else -offset_x
    vertical = offset_y if "PITCH_DOWN" in control else -offset_y
    return min(horizontal, vertical)

def main(ctx):
    observed = action.call(
        id="elite-dangerous/supercruise-target-position",
        inputs={
            "targetName": ctx.inputs["targetName"],
            "scanProfile": "LOS_DIRECTION",
            "reticleEvidencePolicy": "OCCLUSION_AWARE",
        },
    )
    target = observed["target"]
    if target["state"] != "DETECTED":
        return unknown(target, target["reason"])
    if not target["identityConfirmed"]:
        return unknown(target, "CURRENT_TARGET_IDENTITY_NOT_CONFIRMED")
    if target["presentation"] != "DASHED":
        return unknown(target, "DASHED_OCCLUDED_FOCUS_FRAME_REQUIRED")
    if target["reticleEvidencePlane"] != "HSV_ORANGE" and target["reticleEvidencePlane"] != "STRICT_RGB":
        return unknown(target, "LOS_DIRECTION_EVIDENCE_PLANE_REQUIRED")
    if target["shapeConfidencePermille"] < MIN_SHAPE_CONFIDENCE_PERMILLE:
        return unknown(target, "FOCUS_FRAME_SHAPE_CONFIDENCE_LOW")
    if target["focusFrameConfidencePermille"] < MIN_FOCUS_FRAME_CONFIDENCE_PERMILLE:
        return unknown(target, "FOCUS_FRAME_FUSED_CONFIDENCE_LOW")
    if target["centerDistancePixels"] < MIN_DIRECTION_OFFSET_PIXELS:
        return unknown(target, "FOCUS_FRAME_TOO_CLOSE_TO_SCREEN_CENTER_FOR_BYPASS_DIRECTION")

    control = choose_control(target["offsetX"], target["offsetY"])
    if control == None:
        return unknown(target, "FOCUS_FRAME_DIRECTION_AMBIGUOUS")
    projection = initial_projection(control, target["offsetX"], target["offsetY"])
    if projection < MIN_DIRECTION_OFFSET_PIXELS:
        return unknown(target, "FOCUS_FRAME_DOMINANT_COMPONENT_TOO_SMALL")
    return {
        "schemaVersion": 1,
        "direction": {
            "state": "READY",
            "control": control,
            "reason": "DASHED_CURRENT_FRAME_FOCUS_FRAME_OUTWARD_DIRECTION_CONFIRMED:" + target["reticleEvidencePlane"],
            "targetPresentation": target["presentation"],
            "targetOffsetX": target["offsetX"],
            "targetOffsetY": target["offsetY"],
            "targetCenterDistancePixels": target["centerDistancePixels"],
            "initialProjectionPixels": projection,
            "reticleEvidencePlane": target["reticleEvidencePlane"],
            "shapeConfidencePermille": target["shapeConfidencePermille"],
            "focusFrameConfidencePermille": target["focusFrameConfidencePermille"],
            "identityConfirmed": target["identityConfirmed"],
            "reticleCapturedAt": target["reticleCapturedAt"],
        },
    }
