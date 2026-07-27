SUPPORTED_IMAGE_SHA256 = "d55a45f0dda3dc9dc40146d62cd02609941f14c07bc1aa9083d67c0a4807109f"
INVENTORY_MANAGER_PATTERN = "?? 89 ?? ?? ?? ?? ?? ?? 8D ?? 30 01 00 00 ?? 89 ?? ?? ?? ?? ?? ?? 8D ?? B0 01 00 00 ?? 89 ?? ?? ?? ?? ?? ?? 88"
SAVE_DECODER = "crimson-rs/inventory@bb730180"
ITEM_STRIDE = 0xC8

def hex_address(value):
    return "0x%x" % value

def read_pointer(address):
    result = observer.memory.read_batch(
        reads = [{"address": address, "type": "pointer"}],
    )
    return result["reads"][0]["value"]

def resolve_inventory_container(manager):
    value = read_pointer(hex_address(manager + 0x28))
    value = read_pointer(hex_address(value + 0xD0))
    value = read_pointer(hex_address(value + 0x68))
    value = read_pointer(hex_address(value + 0xB8))
    value = read_pointer(hex_address(value + 0x18))
    return read_pointer(hex_address(value + 0x08))

def read_from_memory():
    module_result = observer.memory.modules()
    process = module_result["process"]
    if process["imageSha256"] != SUPPORTED_IMAGE_SHA256:
        return job.fail(
            code = "UNSUPPORTED_BUILD",
            message = "CrimsonDesert.exe SHA-256 is not the reviewed 1.0.0.2145 build",
        )

    executable = None
    for module in module_result["modules"]:
        if module["name"] == "CrimsonDesert.exe":
            executable = module
            break
    if executable == None:
        return job.fail(
            code = "TARGET_MODULE_NOT_FOUND",
            message = "CrimsonDesert.exe is absent from the bound process",
        )

    scan_result = observer.memory.scan(
        pattern = INVENTORY_MANAGER_PATTERN,
        regions = [{
            "base_address": executable["baseAddress"],
            "size": executable["size"],
        }],
        max_matches = 2,
    )
    if len(scan_result["matches"]) != 1:
        return job.fail(
            code = "INVENTORY_SIGNATURE_AMBIGUOUS",
            message = "inventory manager signature must have exactly one match",
        )

    global_result = observer.memory.resolve_rip(
        address = scan_result["matches"][0]["address"],
        displacement_offset = 3,
        instruction_length = 7,
    )
    manager = read_pointer(global_result["targetAddress"])
    container = resolve_inventory_container(manager)
    header = observer.memory.read_batch(
        reads = [
            {"address": hex_address(container), "type": "pointer"},
            {"address": hex_address(container + 0x0C), "type": "u16"},
        ],
    )
    records_address = header["reads"][0]["value"]
    count = header["reads"][1]["value"]
    if records_address == 0 or count > 2048:
        return job.fail(
            code = "INVENTORY_HEADER_INVALID",
            message = "inventory array pointer or item count violates reviewed bounds",
        )

    records_result = observer.memory.read_strided(
        base_address = hex_address(records_address),
        count = count,
        stride = ITEM_STRIDE,
        fields = [
            {"name": "itemId", "offset": 0x08, "type": "u16"},
            {"name": "quantity", "offset": 0x10, "type": "u64"},
            {"name": "pairedItemId", "offset": 0x90, "type": "u16"},
        ],
    )
    items = []
    for record in records_result["records"]:
        if record["itemId"] == 0:
            continue
        items.append({
            "slot": record["index"],
            "itemId": record["itemId"],
            "quantity": record["quantity"],
            "pairedItemId": record["pairedItemId"],
            "inventoryKey": None,
            "instanceId": None,
        })
    return {
        "source": {
            "kind": "process-memory",
            "processImageSha256": process["imageSha256"],
            "saveModifiedAt": None,
            "decoder": None,
        },
        "inventory": {
            "recordCount": count,
            "occupiedCount": len(items),
            "items": items,
        },
    }

def read_from_save():
    decoded = observer.file.decode(
        path = job.input(name = "save"),
        decoder = SAVE_DECODER,
        options = {"scope": "active-character-inventory"},
    )
    payload = decoded["value"]
    items = []
    for record in payload["items"]:
        if record["itemId"] == 0:
            continue
        items.append({
            "slot": record["slot"],
            "itemId": record["itemId"],
            "quantity": record["quantity"],
            "pairedItemId": None,
            "inventoryKey": record["inventoryKey"],
            "instanceId": record["instanceId"],
        })
    return {
        "source": {
            "kind": "save-file",
            "processImageSha256": None,
            "saveModifiedAt": decoded["modifiedAt"],
            "decoder": decoded["decoder"],
        },
        "inventory": {
            "recordCount": payload["recordCount"],
            "occupiedCount": len(items),
            "items": items,
        },
    }

def failed_attempt(attempt):
    return {
        "source": attempt["source"],
        "status": "failed",
        "errorCode": attempt["error"]["code"],
    }

def succeeded_attempt(attempt):
    return {
        "source": attempt["source"],
        "status": "succeeded",
        "errorCode": None,
    }

def finish(attempt, attempts):
    value = attempt["value"]
    return {
        "schemaVersion": 1,
        "source": value["source"],
        "attempts": attempts,
        "inventory": value["inventory"],
    }

def main(ctx):
    memory = job.attempt(source = "process-memory", function = read_from_memory)
    if memory["ok"]:
        return finish(memory, [succeeded_attempt(memory)])

    save = job.attempt(source = "save-file", function = read_from_save)
    attempts = [failed_attempt(memory)]
    if save["ok"]:
        attempts.append(succeeded_attempt(save))
        return finish(save, attempts)

    return job.fail(
        code = "INVENTORY_ALL_SOURCES_FAILED",
        message = "process-memory failed with %s; save-file failed with %s" % (
            memory["error"]["code"],
            save["error"]["code"],
        ),
    )
