SUPPORTED_IMAGE_SHA256 = "d55a45f0dda3dc9dc40146d62cd02609941f14c07bc1aa9083d67c0a4807109f"
INVENTORY_MANAGER_PATTERN = "?? 89 ?? ?? ?? ?? ?? ?? 8D ?? 30 01 00 00 ?? 89 ?? ?? ?? ?? ?? ?? 8D ?? B0 01 00 00 ?? 89 ?? ?? ?? ?? ?? ?? 88"
SAVE_LIBRARY = "save-decoder"
ITEM_STRIDE = 0xC8
CRIMSON_OK = 0
CRIMSON_BUFFER_TOO_SMALL = -11
MAX_INVENTORY_RECORDS = 2048
BACKPACK_INVENTORY_KEY = 2

CRIMSON_RECORD = native.struct(fields = [
    {"name": "blockIndex", "type": native.u32()},
    {"name": "inventoryElementIndex", "type": native.u32()},
    {"name": "itemElementIndex", "type": native.u32()},
    {"name": "inventoryKey", "type": native.u32()},
    {"name": "itemKey", "type": native.u32()},
    {"name": "transferredItemKey", "type": native.u32()},
    {"name": "slotNumber", "type": native.u32()},
    {"name": "flags", "type": native.u32()},
    {"name": "itemNumber", "type": native.u64()},
    {"name": "stackCount", "type": native.u64()},
])

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
    if records_address == 0 or count > MAX_INVENTORY_RECORDS:
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
            "nativeLibrary": None,
        },
        "inventory": {
            "recordCount": count,
            "occupiedCount": len(items),
            "items": items,
        },
    }

def select_save_path():
    listing = observer.file.list(
        path = {
            "root": "crimson-desert-saves",
            "relative": ".",
        },
        maxDepth = 3,
        maxEntries = 4096,
    )
    accounts = []
    for entry in listing["entries"]:
        if entry["kind"] == "reparse-point":
            return job.fail(
                code = "SAVE_REPARSE_POINT_FOUND",
                message = "Crimson Desert save root contains a reparse point",
            )
        parts = entry["relative"].split("/")
        if entry["kind"] == "directory" and len(parts) == 1:
            accounts.append(entry["relative"])
    if len(accounts) == 0:
        return job.fail(
            code = "ACCOUNT_SAVE_ROOT_NOT_FOUND",
            message = "Crimson Desert save root contains no account directory",
        )
    if len(accounts) != 1:
        return job.fail(
            code = "ACCOUNT_SAVE_ROOT_AMBIGUOUS",
            message = "Crimson Desert save root must contain exactly one account directory",
        )

    account = accounts[0]
    candidates = []
    for entry in listing["entries"]:
        parts = entry["relative"].split("/")
        if (
            entry["kind"] == "file" and
            len(parts) == 3 and
            parts[0] == account and
            parts[2] == "save.save"
        ):
            candidates.append(entry)
    if len(candidates) == 0:
        return job.fail(
            code = "SAVE_CANDIDATE_NOT_FOUND",
            message = "account save root contains no <slot>/save.save candidate",
        )

    newest = candidates[0]
    tied = False
    for candidate in candidates[1:]:
        if candidate["modifiedAt"] > newest["modifiedAt"]:
            newest = candidate
            tied = False
        elif candidate["modifiedAt"] == newest["modifiedAt"]:
            tied = True
    if tied:
        return job.fail(
            code = "NEWEST_SAVE_TIMESTAMP_TIE",
            message = "newest save timestamp is shared by multiple candidates",
        )
    return {
        "root": "crimson-desert-saves",
        "relative": newest["relative"],
    }

def read_from_save():
    blob = observer.file.open_blob(
        path = select_save_path(),
    )
    blob_path = native.blob_path(blob = blob["blob"])
    decoder = native.load_library(SAVE_LIBRARY)
    load_file = decoder.bind(
        name = "crimson_save_load_from_file",
        parameters = [native.c_string(), native.out(native.handle())],
        result = native.i32(),
    )
    list_count = decoder.bind(
        name = "crimson_save_list_inventory_items",
        parameters = [
            native.handle(),
            native.pointer(),
            native.usize(),
            native.out(native.usize()),
            native.out(native.u64()),
        ],
        result = native.i32(),
    )
    free_save = decoder.bind(
        name = "crimson_save_free",
        parameters = [native.handle()],
        result = native.void(),
    )

    loaded = load_file.call(blob_path)
    loaded_handle = loaded["out"][0]
    if loaded["result"] != CRIMSON_OK or loaded_handle == 0:
        if loaded_handle != 0:
            free_save.call(loaded_handle)
        return job.fail(
            code = "SAVE_LOAD_FAILED",
            message = "crimson-rs could not load the authorized save blob",
        )
    save_handle = loaded_handle
    query = list_count.call(save_handle, native.null(), 0)
    count = query["out"][0]
    if query["result"] != CRIMSON_OK and query["result"] != CRIMSON_BUFFER_TOO_SMALL:
        free_save.call(save_handle)
        return job.fail(
            code = "SAVE_INVENTORY_QUERY_FAILED",
            message = "crimson-rs could not query inventory record count",
        )
    if count > MAX_INVENTORY_RECORDS:
        free_save.call(save_handle)
        return job.fail(
            code = "SAVE_INVENTORY_COUNT_INVALID",
            message = "save inventory record count exceeds the reviewed bound",
        )

    list_records = decoder.bind(
        name = "crimson_save_list_inventory_items",
        parameters = [
            native.handle(),
            native.out(native.array(CRIMSON_RECORD, count)),
            native.usize(),
            native.out(native.usize()),
            native.out(native.u64()),
        ],
        result = native.i32(),
    )
    listed = list_records.call(save_handle, count)
    free_save.call(save_handle)
    if listed["result"] != CRIMSON_OK:
        return job.fail(
            code = "SAVE_INVENTORY_READ_FAILED",
            message = "crimson-rs could not read inventory records",
        )
    if listed["out"][1] > count:
        return job.fail(
            code = "SAVE_INVENTORY_CHANGED",
            message = "save inventory record count grew during the fixed read",
        )

    records = listed["out"][0]
    items = []
    for record in records:
        if record["inventoryKey"] != BACKPACK_INVENTORY_KEY or record["itemKey"] == 0:
            continue
        items.append({
            "slot": record["slotNumber"],
            "itemId": record["itemKey"],
            "quantity": record["stackCount"],
            "pairedItemId": None,
            "inventoryKey": record["inventoryKey"],
            "instanceId": record["itemNumber"],
        })
    return {
        "source": {
            "kind": "save-file",
            "processImageSha256": None,
            "saveModifiedAt": blob["modifiedAt"],
            "nativeLibrary": SAVE_LIBRARY,
        },
        "inventory": {
            "recordCount": listed["out"][1],
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
