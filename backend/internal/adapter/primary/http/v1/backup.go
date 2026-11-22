package v1

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/TakuyaYagam1/ForcAD/internal/adapter/primary/http/api"
	"github.com/TakuyaYagam1/ForcAD/internal/backup"
)

type BackupHandler struct {
	service BackupService
}

func NewBackupHandler(service BackupService) *BackupHandler {
	return &BackupHandler{
		service: service,
	}
}

func (h *BackupHandler) ExportAdminBackup(ctx context.Context, params api.ExportAdminBackupParams, w http.ResponseWriter) error {
	const location = "httpv1.BackupHandler.ExportAdminBackup"
	name := params.Name.Or("event")
	if name == "" {
		name = "event"
	}
	req := http.Request{
		Method: http.MethodGet,
	}
	if value, ok := params.Range.Get(); ok {
		req.Header = http.Header{
			"Range": {value},
		}
		if value, ok := params.IfRange.Get(); ok {
			req.Header.Set("If-Range", value)
		}
	}
	file, err := os.CreateTemp("", "forcad-export-")
	if err != nil {
		return fmt.Errorf("%s - stage download: %w", location, err)
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	manifest, err := h.service.Export(ctx, file, name)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+manifest.Filename()+`"`)
	http.ServeContent(w, &req, manifest.Filename(), time.Time{}, file)
	return nil
}

func (h *BackupHandler) InspectAdminBackup(ctx context.Context, req api.InspectAdminBackupReq) (api.InspectAdminBackupRes, error) {
	manifest, err := h.service.Inspect(ctx, req)
	if err != nil {
		return nil, err
	}
	return backupManifestResponse(manifest), nil
}

func (h *BackupHandler) RestoreAdminBackup(ctx context.Context, req api.RestoreAdminBackupReq, _ api.RestoreAdminBackupParams) (api.RestoreAdminBackupRes, error) {
	manifest, err := h.service.Restore(ctx, req)
	if err != nil {
		return nil, err
	}
	return backupManifestResponse(manifest), nil
}

func backupManifestResponse(manifest backup.Manifest) *api.BackupManifest {
	files := make(api.BackupManifestFiles, len(manifest.Files))
	for name, entry := range manifest.Files {
		files[name] = api.BackupArchiveEntry{
			Size:   entry.Size,
			SHA256: entry.SHA256,
		}
	}
	return &api.BackupManifest{
		FormatVersion: api.BackupManifestFormatVersion(manifest.Version),
		ForcadVersion: manifest.ForcAD,
		Name:          manifest.Name,
		CreatedAt:     manifest.CreatedAt,
		Round:         manifest.Round,
		Files:         files,
	}
}

func (h *BackupHandler) GetClientTeamLogo(ctx context.Context, params api.GetClientTeamLogoParams, w http.ResponseWriter) error {
	contentType, content, err := h.service.Logo(ctx, params.Name)
	if errors.Is(err, fs.ErrNotExist) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.Error(w, "404 page not found", http.StatusNotFound)
		return nil
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(content)
	return nil
}
