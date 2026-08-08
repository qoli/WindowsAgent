def main(ctx):
    contacts = action.call(id="elite-dangerous/left-panel-tab-state", inputs={})
    if contacts["activeTab"]["state"] not in ["CONTACTS", "UNKNOWN"]:
        return action.call(
            id="elite-dangerous/request-docking-availability-classifier",
            inputs={"contacts": contacts, "regions": None},
        )
    regions = action.call(id="elite-dangerous/request-docking-action-regions", inputs={})
    return action.call(
        id="elite-dangerous/request-docking-availability-classifier",
        inputs={"contacts": contacts, "regions": regions},
    )
