def numeric(value):
    return type(value) == "int" or type(value) == "float"

def parse_system(entry, index):
    if type(entry) != "dict":
        return None
    name = entry.get("StarSystem")
    address = entry.get("SystemAddress")
    position = entry.get("StarPos")
    star_class = entry.get("StarClass")
    if type(name) != "string" or len(name) == 0 or name != name.strip() or len(name) > 96:
        return None
    if type(address) != "int" or address <= 0:
        return None
    if type(position) != "list" or len(position) != 3:
        return None
    for coordinate in position:
        if not numeric(coordinate):
            return None
    if type(star_class) != "string" or len(star_class) == 0 or len(star_class) > 16:
        return None
    result = {
        "name": name,
        "systemAddress": address,
        "starClass": star_class,
        "starPos": position,
    }
    if index > 0:
        result["index"] = index
    return result

def main(ctx):
    raw = ctx.inputs["raw"]
    expected_destination = ctx.inputs["expectedDestinationSystem"]
    max_jumps = ctx.inputs["maxJumps"]
    if raw.get("state") != "AVAILABLE":
        return job.fail(code="ED_NAV_ROUTE_UNAVAILABLE", message="NavRoute.json is absent; plot a route before multi-System transit")
    data = raw.get("data")
    source = raw.get("source")
    if type(data) != "dict" or type(source) != "dict":
        return job.fail(code="ED_NAV_ROUTE_MALFORMED", message="filesystem NavRoute result does not contain data and source objects")
    if data.get("event") == "NavRouteClear":
        return job.fail(code="ED_NAV_ROUTE_CLEARED", message="NavRoute.json reports NavRouteClear; no plotted route is active")
    if data.get("event") != "NavRoute":
        return job.fail(code="ED_NAV_ROUTE_EVENT_INVALID", message="NavRoute.json does not contain the NavRoute event")
    route = data.get("Route")
    if type(route) != "list" or len(route) < 2:
        return job.fail(code="ED_NAV_ROUTE_TOO_SHORT", message="NavRoute must contain an origin and at least one destination System")
    jump_count = len(route) - 1
    if jump_count > max_jumps:
        return job.fail(code="ED_NAV_ROUTE_JUMP_LIMIT", message="NavRoute jump count exceeds the caller-owned maxJumps limit")

    parsed = []
    addresses = {}
    for index in range(len(route)):
        system = parse_system(route[index], index)
        if system == None:
            return job.fail(code="ED_NAV_ROUTE_ENTRY_INVALID", message="NavRoute entry " + str(index) + " is malformed")
        address_key = str(system["systemAddress"])
        if address_key in addresses:
            return job.fail(code="ED_NAV_ROUTE_DUPLICATE_SYSTEM", message="NavRoute contains duplicate SystemAddress " + address_key)
        addresses[address_key] = True
        parsed.append(system)

    destination = parsed[len(parsed) - 1]
    if destination["name"] != expected_destination:
        return job.fail(code="ED_NAV_ROUTE_DESTINATION_MISMATCH", message="NavRoute final System does not match expectedDestinationSystem")
    source_timestamp = source.get("sourceTimestamp")
    if type(source_timestamp) != "string" or len(source_timestamp) == 0:
        return job.fail(code="ED_NAV_ROUTE_SOURCE_INVALID", message="NavRoute sourceTimestamp is missing")
    freshness = raw.get("freshness")
    if freshness != "CURRENT" and freshness != "UNKNOWN":
        return job.fail(code="ED_NAV_ROUTE_FRESHNESS_INVALID", message="NavRoute freshness must be CURRENT or UNKNOWN")

    origin_entry = parsed[0]
    origin = {
        "name": origin_entry["name"],
        "systemAddress": origin_entry["systemAddress"],
        "starClass": origin_entry["starClass"],
        "starPos": origin_entry["starPos"],
    }
    hops = parsed[1:]
    route_id = source_timestamp + ":" + str(destination["systemAddress"]) + ":" + str(jump_count)
    destination_result = {
        "name": destination["name"],
        "systemAddress": destination["systemAddress"],
        "starClass": destination["starClass"],
        "starPos": destination["starPos"],
    }
    return {
        "schemaVersion": 1,
        "state": "READY",
        "routeId": route_id,
        "freshness": freshness,
        "sourceTimestamp": source_timestamp,
        "origin": origin,
        "destination": destination_result,
        "jumpCount": jump_count,
        "hops": hops,
    }
