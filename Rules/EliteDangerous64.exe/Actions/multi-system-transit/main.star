READINESS_LIMIT = 20
READINESS_POLL_MS = 1000

def emit_update(phase, hop_index, jump_count, from_system=None, to_system=None, next_system=None, route_id=None, child_action=None, commanded_throttle=None, fuel_main=None, temperature=None, status_timestamp=None, reason=None):
    stream.emit(
        type="action.multi-system-transit.update",
        payload={
            "phase": phase,
            "hopIndex": hop_index,
            "jumpCount": jump_count,
            "fromSystem": from_system,
            "toSystem": to_system,
            "nextSystem": next_system,
            "routeId": route_id,
            "childAction": child_action,
            "commandedThrottle": commanded_throttle,
            "fuelMain": fuel_main,
            "temperature": temperature,
            "statusTimestamp": status_timestamp,
            "reason": reason,
        },
    )

def read_route_plan(destination_system, max_jumps):
    raw = action.call(id="elite-dangerous/filesystem/nav-route", inputs={})
    return action.call(id="elite-dangerous/nav-route-plan", inputs={
        "raw": raw,
        "expectedDestinationSystem": destination_system,
        "maxJumps": max_jumps,
    })

def wait_readiness(hop_index, jump_count, route_id, from_system, to_system, previous_timestamp, minimum_fuel, require_fuel, expected_target_name, expected_target_address):
    for _ in range(READINESS_LIMIT):
        status = action.call(id="elite-dangerous/filesystem/status", inputs={})
        if status["state"] != "AVAILABLE":
            fail("Status.json is unavailable before route hop " + str(hop_index))
        source = status["source"]
        timestamp = source["sourceTimestamp"]
        if previous_timestamp != None and timestamp == previous_timestamp:
            emit_update("WAITING_READINESS", hop_index, jump_count, from_system=from_system, to_system=to_system, route_id=route_id, commanded_throttle=0, status_timestamp=timestamp, reason="AWAITING_NEW_STATUS_TIMESTAMP")
            task.sleep(milliseconds=READINESS_POLL_MS)
            continue
        data = status["data"]
        fuel = data.get("Fuel")
        if type(fuel) != "dict" or not ("FuelMain" in fuel):
            fail("Status.json Fuel.FuelMain is missing before route hop " + str(hop_index))
        fuel_main = fuel["FuelMain"]
        if type(fuel_main) != "int" and type(fuel_main) != "float":
            fail("Status.json FuelMain is not numeric before route hop " + str(hop_index))
        if require_fuel and fuel_main < minimum_fuel:
            fail("Status.json FuelMain is below minimumFuelMain before route hop " + str(hop_index))
        if expected_target_name != None:
            destination = data.get("Destination")
            if type(destination) != "dict" or destination.get("Name") != expected_target_name or destination.get("System") != expected_target_address:
                fail("Status.json Destination does not match the frozen route target before hop " + str(hop_index))
        return {"fuelMain": fuel_main, "sourceTimestamp": timestamp, "freshness": status["freshness"]}
    fail("route readiness did not become current and safe before the bounded wait limit")

def current_route_progress(hops):
    status = action.call(id="elite-dangerous/filesystem/status", inputs={})
    if status["state"] != "AVAILABLE":
        fail("Status.json is unavailable while resolving current route progress")
    destination = status["data"].get("Destination")
    if type(destination) != "dict":
        fail("Status.json Destination is unavailable while resolving current route progress")
    for hop in hops:
        if destination.get("Name") == hop["name"] and destination.get("System") == hop["systemAddress"]:
            return hop["index"]
    fail("Status.json Destination is not one of the frozen route hops")

def main(ctx):
    destination_system = ctx.inputs["destinationSystem"]
    start_mode = ctx.inputs["startMode"]
    max_jumps = ctx.inputs["maxJumps"]
    minimum_fuel = ctx.inputs["minimumFuelMain"]
    if not ctx.inputs["routeFuelConfirmed"]:
        fail("routeFuelConfirmed must be true before multi-System transit")
    if start_mode == "NORMAL_SPACE" and not ctx.inputs["normalSpaceConfirmed"]:
        fail("normalSpaceConfirmed must be true for NORMAL_SPACE startMode")
    if start_mode == "SUPERCRUISE" and not ctx.inputs["supercruiseConfirmed"]:
        fail("supercruiseConfirmed must be true for SUPERCRUISE startMode")

    action.on_failure(id="elite-dangerous/set-throttle", inputs={"percent": 0}, critical=True, timeout_milliseconds=2000)
    emit_update("PLANNING_ROUTE", 0, 0, to_system=destination_system, commanded_throttle=0, child_action="elite-dangerous/nav-route-plan")
    plan = read_route_plan(destination_system, max_jumps)
    route_id = plan["routeId"]
    jump_count = plan["jumpCount"]
    origin_system = plan["origin"]["name"]
    emit_update("ROUTE_READY", 0, jump_count, from_system=origin_system, to_system=destination_system, route_id=route_id, commanded_throttle=0, reason=plan["freshness"])

    if start_mode == "DOCKED":
        stream.activity(message="Leaving origin station for multi-System route", level="info")
        emit_update("DEPARTURE", 0, jump_count, from_system=origin_system, route_id=route_id, child_action="elite-dangerous/leave-station", commanded_throttle=0)
        action.call(id="elite-dangerous/leave-station", inputs={"stationConfirmed": True})
        emit_update("DEPARTURE_COMPLETED", 0, jump_count, from_system=origin_system, route_id=route_id, child_action="elite-dangerous/leave-station", commanded_throttle=0, reason="NORMAL_SPACE_STOP_CONFIRMED")
        current_mode = "NORMAL_SPACE"
    else:
        current_mode = start_mode

    start_hop_index = current_route_progress(plan["hops"])
    current_system = origin_system if start_hop_index == 1 else plan["hops"][start_hop_index - 2]["name"]
    previous_status_timestamp = None
    carried_readiness = None
    final_readiness = None
    completed_jumps = start_hop_index - 1
    hops = plan["hops"]
    if start_hop_index > 1:
        emit_update("ROUTE_RESUMED", start_hop_index, jump_count, from_system=current_system, to_system=hops[start_hop_index - 1]["name"], route_id=route_id, commanded_throttle=0, reason="STATUS_DESTINATION_MATCHED_FROZEN_HOP")
    for hop in hops:
        hop_index = hop["index"]
        if hop_index < start_hop_index:
            continue
        target_system = hop["name"]
        target_address = hop["systemAddress"]
        next_system = hops[hop_index]["name"] if hop_index < jump_count else None
        next_address = hops[hop_index]["systemAddress"] if hop_index < jump_count else None
        if carried_readiness == None:
            readiness = wait_readiness(hop_index, jump_count, route_id, current_system, target_system, previous_status_timestamp, minimum_fuel, True, target_system, target_address)
        else:
            readiness = carried_readiness
        emit_update("HOP_STARTING", hop_index, jump_count, from_system=current_system, to_system=target_system, next_system=next_system, route_id=route_id, child_action="elite-dangerous/hyperspace-jump-to-system", commanded_throttle=0, fuel_main=readiness["fuelMain"], status_timestamp=readiness["sourceTimestamp"], reason=current_mode + ":STATUS_" + readiness["freshness"])
        jump = action.call(id="elite-dangerous/hyperspace-jump-to-system", inputs={
            "targetSystem": target_system,
            "targetSystemAddress": target_address,
            "targetLockConfirmed": True,
            "startMode": current_mode,
            "normalSpaceConfirmed": current_mode == "NORMAL_SPACE",
            "supercruiseConfirmed": current_mode == "SUPERCRUISE",
        })
        if not jump["completed"] or jump["finalPhase"] != "ARRIVED_IN_SUPERCRUISE" or not jump["arrivalBrakeSent"]:
            fail("hyperspace jump child returned an invalid completion result for route hop " + str(hop_index))
        final_readiness = wait_readiness(hop_index, jump_count, route_id, current_system, target_system, readiness["sourceTimestamp"], minimum_fuel, hop_index < jump_count, next_system, next_address)
        completed_jumps += 1
        emit_update("HOP_COMPLETED", hop_index, jump_count, from_system=current_system, to_system=target_system, next_system=next_system, route_id=route_id, child_action="elite-dangerous/hyperspace-jump-to-system", commanded_throttle=0, fuel_main=final_readiness["fuelMain"], status_timestamp=final_readiness["sourceTimestamp"], reason="ARRIVED_IN_SUPERCRUISE:STATUS_" + final_readiness["freshness"])

        emit_update("REVALIDATING_ROUTE", hop_index, jump_count, from_system=current_system, to_system=target_system, next_system=next_system, route_id=route_id, child_action="elite-dangerous/nav-route-plan", commanded_throttle=0)
        current_plan = read_route_plan(destination_system, max_jumps)
        if current_plan["routeId"] != route_id:
            fail("NavRoute identity changed during multi-System transit")
        emit_update("ROUTE_REVALIDATED", hop_index, jump_count, from_system=current_system, to_system=target_system, next_system=next_system, route_id=route_id, child_action="elite-dangerous/nav-route-plan", commanded_throttle=0, reason=current_plan["freshness"])
        previous_status_timestamp = final_readiness["sourceTimestamp"]
        carried_readiness = final_readiness
        current_system = target_system
        current_mode = "SUPERCRUISE"

    if completed_jumps != jump_count or current_system != destination_system:
        fail("multi-System transit did not consume the complete frozen route")
    action.clear_on_failure()
    stream.activity(message="Final route System reached at 0% throttle", level="info")
    emit_update("FINAL_SYSTEM_REACHED", completed_jumps, jump_count, from_system=origin_system, to_system=destination_system, route_id=route_id, commanded_throttle=0, fuel_main=final_readiness["fuelMain"], status_timestamp=final_readiness["sourceTimestamp"], reason="ALL_ROUTE_HOPS_COMPLETED")
    return {
        "schemaVersion": 1,
        "task": "MULTI_SYSTEM_TRANSIT",
        "completed": True,
        "finalPhase": "FINAL_SYSTEM_REACHED",
        "routeId": route_id,
        "originSystem": origin_system,
        "destinationSystem": destination_system,
        "jumpCount": jump_count,
        "completedJumps": completed_jumps,
        "finalFuelMain": final_readiness["fuelMain"],
        "finalStatusTimestamp": final_readiness["sourceTimestamp"],
        "visualConfirmed": False,
    }
