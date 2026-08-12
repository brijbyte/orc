import { Trash2, X } from "lucide-react";
import { AlertDialog } from "@base-ui/react/alert-dialog";
import { sessionTitle } from "../lib/session";
import type { SessionRow } from "../lib/types";
import { Button } from "../ui/Button";
import d from "../ui/dialog.module.css";

export function DeleteDialog({
  open,
  row,
  onOpenChange,
  onClosed,
  onDelete,
}: {
  open: boolean;
  row: SessionRow | null;
  onOpenChange: (open: boolean) => void;
  onClosed: () => void;
  onDelete: (row: SessionRow) => void;
}) {
  return (
    <AlertDialog.Root
      open={open}
      onOpenChange={onOpenChange}
      onOpenChangeComplete={(isOpen) => !isOpen && onClosed()}
    >
      <AlertDialog.Portal>
        <AlertDialog.Backdrop className={d.overlay} />
        <AlertDialog.Popup className={`${d.popup} ${d.confirm}`}>
          <AlertDialog.Title className={d.head}>
            Delete session?
          </AlertDialog.Title>
          <AlertDialog.Description className={d.desc}>
            “<span title={sessionTitle(row)}>{sessionTitle(row)}</span>” and its
            file will be removed.
          </AlertDialog.Description>
          <div className={d.foot}>
            <AlertDialog.Close render={<Button outline />}>
              <X size={13} strokeWidth={1.8} aria-hidden />
              cancel
            </AlertDialog.Close>
            <Button
              outline
              tone="danger"
              disabled={!row}
              onClick={() => row && onDelete(row)}
            >
              <Trash2 size={13} strokeWidth={1.8} aria-hidden />
              delete
            </Button>
          </div>
        </AlertDialog.Popup>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}
