import { useRef, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { LoaderCircle } from "lucide-react";
import { Link } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  activateTeamsAdmin,
  deactivateTeamsAdmin,
  fetchTeamsAdmin,
} from "@/entities/team/api/admin";
import {
  activateTasksAdmin,
  deactivateTasksAdmin,
  fetchTasksAdmin,
} from "@/entities/task/api/admin";
import { apiErrorMessage } from "@/shared/lib/apiError";
import {
  queryClient,
  refreshGameQueries,
} from "@/shared/lib/queryClient";
import { useTranslation } from "react-i18next";
import { useAdminNotification } from "@/shared/ui/admin-notification/useAdminNotification";

export type AdminEntityKind = "teams" | "services";

interface AdminEntityItem {
  id: string | null;
  name: string;
  active: boolean;
}

type BatchAction = "activate" | "deactivate";

interface AdminEntityConfig {
  queryKey: readonly [string];
  fetch: (signal?: AbortSignal) => Promise<AdminEntityItem[]>;
  activate: (ids: string[]) => Promise<{
    activated: number;
  }>;
  deactivate: (ids: string[]) => Promise<{
    deactivated: number;
  }>;
  editPath: string;
}

interface SelectionState {
  kind: AdminEntityKind;
  action: BatchAction | null;
  ids: Set<string>;
}

const MAX_BATCH_SIZE = 100;

const entityConfigs: Record<AdminEntityKind, AdminEntityConfig> = {
  teams: {
    queryKey: ["admin-teams"],
    fetch: fetchTeamsAdmin,
    activate: activateTeamsAdmin,
    deactivate: deactivateTeamsAdmin,
    editPath: "/admin/team/",
  },
  services: {
    queryKey: ["admin-tasks"],
    fetch: fetchTasksAdmin,
    activate: activateTasksAdmin,
    deactivate: deactivateTasksAdmin,
    editPath: "/admin/task/",
  },
};

interface AdminEntityListsProps {
  kind: AdminEntityKind;
}

export function AdminEntityLists({ kind }: AdminEntityListsProps) {
  const { t } = useTranslation("admin");
  const notify = useAdminNotification();
  const config = entityConfigs[kind];
  const cancelRef = useRef<HTMLButtonElement>(null);
  const [selection, setSelection] = useState<SelectionState>({
    kind,
    action: null,
    ids: new Set(),
  });
  const [confirmationOpen, setConfirmationOpen] = useState(false);
  const [requestError, setRequestError] = useState<unknown | null>(null);
  const query = useQuery({
    queryKey: config.queryKey,
    queryFn: ({ signal }) => {
      return config.fetch(signal)
    },
    refetchInterval: 10000,
  });
  const activeItems = query.data?.filter((item) => {
    return item.active && item.id !== null
  }) ?? [];
  const inactiveItems = query.data?.filter((item) => {
    return !item.active && item.id !== null
  }) ?? [];
  const activeIds = new Set(activeItems.flatMap((item) => {
    return item.id === null ? [] : [item.id]
  }));
  const inactiveIds = new Set(inactiveItems.flatMap((item) => {
    return item.id === null ? [] : [item.id]
  }));
  const selectedAction = selection.kind === kind ? selection.action : null;
  const eligibleIds = selectedAction === "activate" ? inactiveIds : activeIds;
  const selectedIds = new Set(
    selection.kind === kind
      ? [...selection.ids].filter((id) => {
        return eligibleIds.has(id)
      })
      : [],
  );
  const selectActiveItems = activeItems.slice(0, MAX_BATCH_SIZE);
  const selectInactiveItems = inactiveItems.slice(0, MAX_BATCH_SIZE);
  const selectedItemIds = (selectedAction === "activate"
    ? inactiveItems
    : activeItems
  ).flatMap((item) => {
    return item.id !== null && selectedIds.has(item.id) ? [item.id] : []
  });
  const selectionCount = selectedItemIds.length;
  const allActiveSelected = selectedAction === "deactivate"
    && selectActiveItems.length > 0
    && selectActiveItems.every((item) => {
      return item.id !== null && selectedIds.has(item.id)
    });
  const allInactiveSelected = selectedAction === "activate"
    && selectInactiveItems.length > 0
    && selectInactiveItems.every((item) => {
      return item.id !== null && selectedIds.has(item.id)
    });

  const mutation = useMutation({
    mutationFn: async (variables: {
      kind: AdminEntityKind;
      action: BatchAction;
      ids: string[];
    }) => {
      const mutationConfig = entityConfigs[variables.kind];
      if (variables.action === "activate") {
        const result = await mutationConfig.activate(variables.ids);
        return result.activated
      }
      const result = await mutationConfig.deactivate(variables.ids);
      return result.deactivated
    },
    onMutate: async (variables) => {
      setRequestError(null);
      const mutationConfig = entityConfigs[variables.kind];
      await queryClient.cancelQueries({
        queryKey: mutationConfig.queryKey,
        exact: true,
      });
      const previousItems = queryClient.getQueryData<AdminEntityItem[]>(
        mutationConfig.queryKey,
      );
      const ids = new Set(variables.ids);
      queryClient.setQueryData<AdminEntityItem[]>(
        mutationConfig.queryKey,
        (items) => {
          return items?.map((item) => {
            return item.id !== null && ids.has(item.id)
              ? {
                ...item,
                active: variables.action === "activate",
              }
              : item
          })
        },
      );
      return {
        previousItems,
      }
    },
    onSuccess: (changed, variables) => {
      setSelection((current) => {
        return current.kind === variables.kind
          ? {
            kind: current.kind,
            action: null,
            ids: new Set(),
          }
          : current
      });
      setConfirmationOpen(false);
      if (variables.action === "activate") {
        notify(t(
          variables.kind === "teams"
            ? "entities.activatedTeams"
            : "entities.activatedServices",
          {
            count: changed,
          },
        ));
      } else {
        notify(t(
          variables.kind === "teams"
            ? "entities.deactivatedTeams"
            : "entities.deactivatedServices",
          {
            count: changed,
          },
        ));
      }
    },
    onError: (cause, variables, context) => {
      if (context?.previousItems !== undefined) {
        queryClient.setQueryData(
          entityConfigs[variables.kind].queryKey,
          context.previousItems,
        );
      }
      setRequestError(cause);
    },
    onSettled: (_data, _error, variables) => {
      refreshGameQueries(queryClient);
      void queryClient.refetchQueries({
        queryKey: entityConfigs[variables.kind].queryKey,
        exact: true,
      });
      void queryClient.refetchQueries({
        queryKey: ["scoreboardInit"],
        exact: true,
      });
    },
    retry: false,
  });

  const toggleItem = (
    id: string,
    checked: boolean,
    action: BatchAction,
  ) => {
    setSelection((current) => {
      const validIds = action === "activate" ? inactiveIds : activeIds;
      const ids = new Set(
        current.kind === kind && current.action === action
          ? [...current.ids].filter((selectedId) => {
            return validIds.has(selectedId)
          })
          : [],
      );
      if (checked && ids.size < MAX_BATCH_SIZE) {
        ids.add(id);
      } else if (!checked) {
        ids.delete(id);
      }
      return {
        kind,
        action: ids.size > 0 ? action : null,
        ids,
      }
    });
  };

  const toggleAll = (action: BatchAction) => {
    const items = action === "activate"
      ? selectInactiveItems
      : selectActiveItems;
    const allSelected = action === "activate"
      ? allInactiveSelected
      : allActiveSelected;
    setSelection({
      kind,
      action: allSelected ? null : action,
      ids: allSelected
        ? new Set()
        : new Set(items.flatMap((item) => {
          return item.id === null ? [] : [item.id]
        })),
    });
  };

  const pending = mutation.isPending;
  const confirmationAction = pending
    ? mutation.variables?.action ?? selectedAction
    : selectedAction;
  const confirmationCount = pending
    ? mutation.variables?.ids.length ?? selectionCount
    : selectionCount;
  const errorMessage = requestError === null
    ? null
    : apiErrorMessage(
      requestError,
      t(
        confirmationAction === "activate"
          ? "entities.activateError"
          : "entities.deactivateError",
      ),
    );

  const openConfirmation = (action: BatchAction) => {
    setRequestError(null);
    if (selectedAction === action && selectionCount > 0) {
      setConfirmationOpen(true);
    }
  };

  return (
    <section className="admin-form admin-entity-list">
      <p className="admin-entity-list__intro">
        {t("entities.disabledDescription")}
      </p>
      <div
        className="admin-entity-list__actions"
        role="group"
        aria-label={t("entities.selectionControls")}
      >
        <div className="admin-entity-list__action">
          <label className="admin-entity-list__select">
            <Checkbox
              aria-label={t("entities.selectActive", {
                count: selectActiveItems.length,
              })}
              checked={allActiveSelected
                ? true
                : selectedAction === "deactivate" && selectionCount > 0
                  ? "indeterminate"
                  : false}
              disabled={pending || selectActiveItems.length === 0}
              onCheckedChange={() => {
                toggleAll("deactivate");
              }}
            />
            <span>
              {t("entities.selectActive", {
                count: selectActiveItems.length,
              })}
            </span>
          </label>
          <Button
            type="button"
            variant="destructive"
            disabled={
              pending
              || selectedAction !== "deactivate"
              || selectionCount === 0
            }
            onClick={() => {
              openConfirmation("deactivate");
            }}
          >
            {t("entities.deactivateSelected", {
              count: selectedAction === "deactivate" ? selectionCount : 0,
            })}
          </Button>
        </div>
        <div className="admin-entity-list__action">
          <label className="admin-entity-list__select">
            <Checkbox
              aria-label={t("entities.selectInactive", {
                count: selectInactiveItems.length,
              })}
              checked={allInactiveSelected
                ? true
                : selectedAction === "activate" && selectionCount > 0
                  ? "indeterminate"
                  : false}
              disabled={pending || selectInactiveItems.length === 0}
              onCheckedChange={() => {
                toggleAll("activate");
              }}
            />
            <span>
              {t("entities.selectInactive", {
                count: selectInactiveItems.length,
              })}
            </span>
          </label>
          <Button
            type="button"
            disabled={
              pending
              || selectedAction !== "activate"
              || selectionCount === 0
            }
            onClick={() => {
              openConfirmation("activate");
            }}
          >
            {t("entities.activateSelected", {
              count: selectedAction === "activate" ? selectionCount : 0,
            })}
          </Button>
        </div>
      </div>
      {(activeItems.length > MAX_BATCH_SIZE
        || inactiveItems.length > MAX_BATCH_SIZE) && (
        <p className="admin-entity-list__warning">
          {t("entities.batchLimit", {
            count: MAX_BATCH_SIZE,
          })}
        </p>
      )}
      {query.isPending && <p>{t("common.loading")}</p>}
      {query.error && (
        <div role="alert">
          {apiErrorMessage(query.error, t("entities.loadError"))}{" "}
          <button
            type="button"
            disabled={pending}
            onClick={() => {
              return void query.refetch()
            }}
          >
            {t("common.retry")}
          </button>
        </div>
      )}
      <ul className="admin-entity-list__items">
        {query.data?.map((item) => {
          const id = item.id;
          const action = item.active ? "deactivate" : "activate";
          const checked = id !== null
            && selectedAction === action
            && selectedIds.has(id);
          return (
            <li key={id} className="admin-entity-list__row">
              <div className="admin-entity-list__identity">
                {id !== null && (
                  <Checkbox
                    aria-label={t("entities.selectItem", {
                      name: item.name,
                    })}
                    checked={checked}
                    disabled={
                      pending
                      || (
                        !checked
                        && selectedAction === action
                        && selectionCount >= MAX_BATCH_SIZE
                      )
                    }
                    onCheckedChange={(nextChecked) => {
                      toggleItem(id, nextChecked === true, action);
                    }}
                  />
                )}
                <Link
                  to={`${config.editPath}${id}`}
                  className={
                    pending
                      ? "pointer-events-none truncate opacity-50"
                      : "truncate hover:underline"
                  }
                  aria-disabled={pending}
                  tabIndex={pending ? -1 : undefined}
                  onClick={(event) => {
                    if (pending) {
                      event.preventDefault();
                    }
                  }}
                >
                  {item.name}
                </Link>
              </div>
              <span
                className={
                  item.active ? "admin-entity-list__state admin-entity-list__state--active" : "admin-entity-list__state"
                }
              >
                {item.active ? t("common.active") : t("common.inactive")}
              </span>
            </li>
          )
        })}
      </ul>
      {query.data?.length === 0 && <p>{t("entities.empty")}</p>}
      <Dialog
        open={confirmationOpen}
        onOpenChange={(open) => {
          if (!pending) {
            setConfirmationOpen(open);
          }
        }}
      >
        <DialogContent
          className="admin-dialog"
          onOpenAutoFocus={(event) => {
            event.preventDefault();
            cancelRef.current?.focus();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {confirmationAction === "activate"
                ? t("entities.confirmActivateTitle", {
                  count: confirmationCount,
                })
                : t("entities.confirmDeactivateTitle", {
                  count: confirmationCount,
                })}
            </DialogTitle>
            <DialogDescription>
              {t(
                confirmationAction === "activate"
                  ? "entities.confirmActivateDescription"
                  : "entities.confirmDeactivateDescription",
              )}
            </DialogDescription>
          </DialogHeader>
          {errorMessage && (
            <p className="text-sm text-red-300" role="alert">
              {errorMessage}
            </p>
          )}
          <DialogFooter className="admin-dialog-actions">
            <Button
              ref={cancelRef}
              type="button"
              variant="outline"
              disabled={pending}
              onClick={() => {
                setConfirmationOpen(false);
              }}
            >
              {t("common.cancel")}
            </Button>
            <Button
              type="button"
              variant={
                confirmationAction === "deactivate"
                  ? "destructive"
                  : "default"
              }
              disabled={
                pending
                || confirmationAction === null
                || confirmationCount === 0
              }
              onClick={() => {
                if (
                  selectedAction !== null
                  && selectedItemIds.length > 0
                ) {
                  mutation.mutate({
                    kind,
                    action: selectedAction,
                    ids: selectedItemIds,
                  });
                }
              }}
            >
              {pending && <LoaderCircle className="animate-spin" />}
              {pending
                ? t(
                  confirmationAction === "activate"
                    ? "entities.activating"
                    : "entities.deactivating",
                )
                : confirmationAction === "activate"
                  ? t("entities.confirmActivate", {
                    count: confirmationCount,
                  })
                  : t("entities.confirmDeactivate", {
                    count: confirmationCount,
                  })}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  );
}
