def main(ctx):
    contacts = action.call(id="elite-dangerous/contacts-tab-state", inputs={})
    if contacts["contactsTab"]["state"] != "SELECTED":
        return action.call(
            id="elite-dangerous/request-docking-availability-classifier",
            inputs={"contacts": contacts, "regions": None},
        )
    regions = action.call(id="elite-dangerous/request-docking-action-regions", inputs={})
    return action.call(
        id="elite-dangerous/request-docking-availability-classifier",
        inputs={"contacts": contacts, "regions": regions},
    )
