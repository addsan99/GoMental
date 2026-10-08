package application

import (
	"context"
	"errors"

	"GoMental/internal/workspace"
)

// CreateFolderRequest names a new folder and the existing folder to put it in.
// An empty Parent is the workspace root.
type CreateFolderRequest struct {
	Parent string `json:"parent"`
	Name   string `json:"name"`
}

// FolderDTO is a folder's workspace-relative path together with its absolute
// location on disk, so a caller can both place it in the tree and reveal it.
type FolderDTO struct {
	Folder string `json:"folder"`
	Path   string `json:"path"`
}

// FolderPath returns the absolute on-disk path of a workspace folder, for "copy
// as path" and reveal-in-file-manager. Like NoteFilePath it resolves in Go
// because a composite maps each top-level folder onto a different member root.
func (s *Service) FolderPath(ctx context.Context, folder string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return "", err
	}
	path, err := ws.PathForFolder(folder)
	if err != nil {
		return "", appErr("folders.invalid_path", "Could not resolve the folder path", err)
	}
	return path, nil
}

// CreateFolder makes an empty folder inside an existing one.
//
// Nothing is indexed: an empty folder holds no notes, so the search index and
// graph have nothing to learn about it until a note is filed there, at which
// point the ordinary note-created path covers it.
func (s *Service) CreateFolder(ctx context.Context, req CreateFolderRequest) (FolderDTO, error) {
	select {
	case <-ctx.Done():
		return FolderDTO{}, ctx.Err()
	default:
	}
	ws, err := s.workspaceSnapshot()
	if err != nil {
		return FolderDTO{}, err
	}
	folder, err := ws.CreateFolder(req.Parent, req.Name)
	if err != nil {
		if errors.Is(err, workspace.ErrFolderAlreadyExists) {
			return FolderDTO{}, appErr("folders.exists", "A folder with that name already exists", err)
		}
		if errors.Is(err, workspace.ErrInvalidFolder) {
			return FolderDTO{}, appErr("folders.invalid_name", "That folder name cannot be used", err)
		}
		return FolderDTO{}, appErr("folders.create_failed", "Could not create the folder", err)
	}
	path, err := ws.PathForFolder(folder)
	if err != nil {
		return FolderDTO{}, appErr("folders.invalid_path", "Could not resolve the folder path", err)
	}
	return FolderDTO{Folder: folder, Path: path}, nil
}
