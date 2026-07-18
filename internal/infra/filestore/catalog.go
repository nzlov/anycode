package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/nzlov/anycode/internal/domain/session"
)

const artifactIDPrefix = "artifact."

func (s *Store) InspectArtifact(ctx context.Context, input session.InspectArtifactInput) (session.SessionFile, error) {
	if err := ctx.Err(); err != nil {
		return session.SessionFile{}, &Error{Code: "canceled", Path: input.SourcePath, Err: err}
	}
	rootPath, err := filepath.Abs(s.ArtifactDir(input.SessionID))
	if err != nil {
		return session.SessionFile{}, &Error{Code: "invalid_output_root", Path: input.SourcePath, Err: err}
	}
	path, err := filepath.Abs(input.SourcePath)
	if err != nil || !pathWithin(rootPath, path) {
		return session.SessionFile{}, &Error{Code: "outside_output_root", Path: input.SourcePath, Err: err}
	}
	logicalPath, err := filepath.Rel(rootPath, path)
	if err != nil {
		return session.SessionFile{}, &Error{Code: "invalid_output_path", Path: path, Err: err}
	}
	logicalPath, err = normalizeArtifactPath(filepath.ToSlash(logicalPath))
	if err != nil {
		return session.SessionFile{}, err
	}
	root, err := s.openArtifactRoot(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	defer root.Close()
	artifact, err := s.artifactFromFile(root, input.SessionID, logicalPath)
	if err != nil {
		return session.SessionFile{}, err
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultArtifactMaxBytes
	}
	if artifact.Size > maxBytes {
		return session.SessionFile{}, &Error{Code: "file_too_large", Path: path}
	}
	return artifact, nil
}

func (s *Store) WriteInlineArtifact(ctx context.Context, input session.WriteInlineArtifactInput) (session.SessionFile, error) {
	if err := ctx.Err(); err != nil {
		return session.SessionFile{}, &Error{Code: "canceled", Err: err}
	}
	maxBytes := input.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultArtifactMaxBytes
	}
	if int64(len(input.Data)) > maxBytes {
		return session.SessionFile{}, &Error{Code: "file_too_large"}
	}
	rootPath, err := s.EnsureArtifactDir(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	root, err := s.openArtifactRoot(ctx, input.SessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	defer root.Close()

	keyHash := sha256.Sum256([]byte(input.SourceKey))
	logicalPath := filepath.ToSlash(filepath.Join("inline", hex.EncodeToString(keyHash[:]), cleanFilename(input.Filename)))
	artifactPath := filepath.Join(rootPath, filepath.FromSlash(logicalPath))
	relativeDir := pathpkg.Dir(logicalPath)
	currentDir := ""
	for _, component := range strings.Split(relativeDir, "/") {
		currentDir = pathpkg.Join(currentDir, component)
		info, statErr := root.Lstat(currentDir)
		if errors.Is(statErr, os.ErrNotExist) {
			break
		}
		if statErr != nil {
			return session.SessionFile{}, &Error{Code: classify(statErr), Path: filepath.Join(rootPath, filepath.FromSlash(currentDir)), Err: statErr}
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return session.SessionFile{}, &Error{Code: "symlink_rejected", Path: filepath.Join(rootPath, filepath.FromSlash(currentDir))}
		}
	}
	if info, statErr := root.Lstat(logicalPath); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return session.SessionFile{}, &Error{Code: "symlink_rejected", Path: artifactPath}
		}
		return s.artifactFromFile(root, input.SessionID, logicalPath)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return session.SessionFile{}, &Error{Code: classify(statErr), Path: artifactPath, Err: statErr}
	}
	if err := root.MkdirAll(relativeDir, 0o755); err != nil {
		return session.SessionFile{}, &Error{Code: classify(err), Path: artifactPath, Err: err}
	}
	partialPath := artifactPath + ".partial"
	partialLogicalPath := logicalPath + ".partial"
	file, err := root.OpenFile(partialLogicalPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return session.SessionFile{}, &Error{Code: classify(err), Path: partialPath, Err: err}
	}
	written, writeErr := file.Write(input.Data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || written != len(input.Data) || syncErr != nil || closeErr != nil {
		_ = root.Remove(partialLogicalPath)
		return session.SessionFile{}, &Error{Code: "write_failed", Path: partialPath, Err: errors.Join(writeErr, syncErr, closeErr)}
	}
	if err := root.Rename(partialLogicalPath, logicalPath); err != nil {
		_ = root.Remove(partialLogicalPath)
		return session.SessionFile{}, &Error{Code: classify(err), Path: artifactPath, Err: err}
	}
	return s.artifactFromFile(root, input.SessionID, logicalPath)
}

func (s *Store) FindArtifact(ctx context.Context, id session.SessionFileID) (session.SessionFile, error) {
	sessionID, digest, ok := decodeArtifactID(id)
	if !ok {
		return session.SessionFile{}, session.ErrSessionFileNotFound
	}
	root, err := s.openArtifactRoot(ctx, sessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	defer root.Close()
	logicalPath, err := findArtifactPath(ctx, root, digest)
	if err != nil {
		return session.SessionFile{}, err
	}
	return s.artifactFromFile(root, sessionID, logicalPath)
}

func (s *Store) ListArtifacts(ctx context.Context, query session.ArtifactQuery) ([]session.SessionFile, error) {
	artifacts := make([]session.SessionFile, 0)
	err := s.walkArtifacts(ctx, query.SessionID, func(root *os.Root, logicalPath string, _ os.FileInfo) error {
		artifact, err := s.artifactFromFile(root, query.SessionID, logicalPath)
		if err != nil {
			return err
		}
		if query.Kind != "" && artifact.ArtifactKind != query.Kind {
			return nil
		}
		if query.Source != "" && artifact.SourceType != query.Source {
			return nil
		}
		filter := strings.ToLower(strings.TrimSpace(query.Filter))
		if filter != "" && !strings.Contains(strings.ToLower(artifact.Filename), filter) && !strings.Contains(strings.ToLower(artifact.LogicalPath), filter) {
			return nil
		}
		artifacts = append(artifacts, artifact)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(artifacts, func(i, j int) bool {
		switch query.Sort {
		case "created_at_asc":
			if artifacts[i].CreatedAt.Equal(artifacts[j].CreatedAt) {
				return artifacts[i].ID < artifacts[j].ID
			}
			return artifacts[i].CreatedAt.Before(artifacts[j].CreatedAt)
		case "filename_asc":
			if artifacts[i].Filename == artifacts[j].Filename {
				return artifacts[i].ID < artifacts[j].ID
			}
			return artifacts[i].Filename < artifacts[j].Filename
		case "size_desc":
			if artifacts[i].Size == artifacts[j].Size {
				return artifacts[i].ID > artifacts[j].ID
			}
			return artifacts[i].Size > artifacts[j].Size
		default:
			if artifacts[i].CreatedAt.Equal(artifacts[j].CreatedAt) {
				return artifacts[i].ID > artifacts[j].ID
			}
			return artifacts[i].CreatedAt.After(artifacts[j].CreatedAt)
		}
	})
	return artifacts, nil
}

func (s *Store) ResolveArtifacts(ctx context.Context, sessionID session.ID, logicalPaths []string) ([]session.SessionFile, error) {
	resolved := make([]session.SessionFile, 0, len(logicalPaths))
	root, err := s.openArtifactRoot(ctx, sessionID)
	if errors.Is(err, session.ErrSessionFileNotFound) {
		return resolved, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, logicalPath := range logicalPaths {
		normalized, err := normalizeArtifactPath(logicalPath)
		if err != nil {
			return nil, err
		}
		artifact, err := s.artifactFromFile(root, sessionID, normalized)
		if errors.Is(err, os.ErrNotExist) || isFilestoreNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, artifact)
	}
	return resolved, nil
}

func (s *Store) SumArtifactSize(ctx context.Context, sessionID session.ID) (int64, error) {
	var total int64
	err := s.walkArtifacts(ctx, sessionID, func(_ *os.Root, _ string, info os.FileInfo) error {
		total += info.Size()
		return nil
	})
	return total, err
}

func (s *Store) CountArtifacts(ctx context.Context, sessionID session.ID) (int, error) {
	count := 0
	err := s.walkArtifacts(ctx, sessionID, func(_ *os.Root, _ string, _ os.FileInfo) error {
		count++
		return nil
	})
	return count, err
}

func (s *Store) WatchArtifactDir(ctx context.Context, sessionID session.ID) (<-chan struct{}, error) {
	if _, err := s.EnsureArtifactDir(ctx, sessionID); err != nil {
		return nil, err
	}
	state, err := s.artifactDirectoryState(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	interval := s.watchInterval
	if interval <= 0 {
		interval = defaultWatchInterval
	}
	changes := make(chan struct{}, 1)
	go func() {
		defer close(changes)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				next, err := s.artifactDirectoryState(ctx, sessionID)
				if err != nil || next == state {
					continue
				}
				state = next
				select {
				case changes <- struct{}{}:
				default:
				}
			}
		}
	}()
	return changes, nil
}

func (s *Store) DeleteArtifact(ctx context.Context, id session.SessionFileID) (session.SessionFile, error) {
	sessionID, digest, ok := decodeArtifactID(id)
	if !ok {
		return session.SessionFile{}, session.ErrSessionFileNotFound
	}
	root, err := s.openArtifactRoot(ctx, sessionID)
	if err != nil {
		return session.SessionFile{}, err
	}
	defer root.Close()
	logicalPath, err := findArtifactPath(ctx, root, digest)
	if err != nil {
		return session.SessionFile{}, err
	}
	artifact, err := s.artifactFromFile(root, sessionID, logicalPath)
	if err != nil {
		return session.SessionFile{}, err
	}
	if err := root.Remove(logicalPath); err != nil {
		return session.SessionFile{}, &Error{Code: classify(err), Path: artifact.Path, Err: err}
	}
	for parent := pathpkg.Dir(logicalPath); parent != "."; parent = pathpkg.Dir(parent) {
		if err := root.Remove(parent); err != nil {
			break
		}
	}
	return artifact, nil
}

func (s *Store) OpenArtifact(ctx context.Context, id session.SessionFileID) (session.AttachmentStream, error) {
	sessionID, digest, ok := decodeArtifactID(id)
	if !ok {
		return session.AttachmentStream{}, session.ErrSessionFileNotFound
	}
	root, err := s.openArtifactRoot(ctx, sessionID)
	if err != nil {
		return session.AttachmentStream{}, err
	}
	defer root.Close()
	logicalPath, err := findArtifactPath(ctx, root, digest)
	if err != nil {
		return session.AttachmentStream{}, err
	}
	path := filepath.Join(s.ArtifactDir(sessionID), filepath.FromSlash(logicalPath))
	file, info, err := openArtifactFile(root, logicalPath, path)
	if err != nil {
		return session.AttachmentStream{}, err
	}
	artifact := s.artifactFromOpenFile(sessionID, logicalPath, path, file, info)
	if _, err := file.Seek(0, 0); err != nil {
		_ = file.Close()
		return session.AttachmentStream{}, &Error{Code: classify(err), Path: path, Err: err}
	}
	return session.AttachmentStream{
		Filename: artifact.Filename,
		MimeType: artifact.MimeType,
		Reader:   file,
		Seeker:   file,
	}, nil
}

func (s *Store) openArtifactRoot(ctx context.Context, sessionID session.ID) (*os.Root, error) {
	return s.artifactRoot(ctx, sessionID, false)
}

func (s *Store) createArtifactRoot(ctx context.Context, sessionID session.ID) (*os.Root, error) {
	return s.artifactRoot(ctx, sessionID, true)
}

func (s *Store) artifactRoot(ctx context.Context, sessionID session.ID, create bool) (*os.Root, error) {
	if err := ctx.Err(); err != nil {
		return nil, &Error{Code: "canceled", Err: err}
	}
	attachmentsPath := s.attachmentsRoot()
	if create {
		if err := os.MkdirAll(attachmentsPath, 0o755); err != nil {
			return nil, &Error{Code: classify(err), Path: attachmentsPath, Err: err}
		}
	}
	attachments, err := os.OpenRoot(attachmentsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, session.ErrSessionFileNotFound
	}
	if err != nil {
		return nil, &Error{Code: classify(err), Path: attachmentsPath, Err: err}
	}
	defer attachments.Close()
	if err := validateOpenedRootPath(attachments, attachmentsPath); err != nil {
		return nil, &Error{Code: "symlink_rejected", Path: attachmentsPath, Err: err}
	}
	outputsPath := filepath.Join(attachmentsPath, "outputs")
	outputs, err := openArtifactChildRoot(attachments, "outputs", outputsPath, create)
	if err != nil {
		return nil, err
	}
	defer outputs.Close()
	sessionDir := safeSessionPathComponent(sessionID)
	return openArtifactChildRoot(outputs, sessionDir, s.ArtifactDir(sessionID), create)
}

func openArtifactChildRoot(parent *os.Root, name string, path string, create bool) (*os.Root, error) {
	if create {
		if err := parent.Mkdir(name, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, &Error{Code: classify(err), Path: path, Err: err}
		}
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		info, infoErr := parent.Lstat(name)
		if infoErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return nil, &Error{Code: "symlink_rejected", Path: path, Err: err}
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, session.ErrSessionFileNotFound
		}
		return nil, &Error{Code: classify(err), Path: path, Err: err}
	}
	info, infoErr := parent.Lstat(name)
	if err := validateOpenedRoot(root, info, infoErr); err != nil {
		_ = root.Close()
		return nil, &Error{Code: "symlink_rejected", Path: path, Err: err}
	}
	return root, nil
}

func validateOpenedRootPath(root *os.Root, path string) error {
	info, err := os.Lstat(path)
	return validateOpenedRoot(root, info, err)
}

func validateOpenedRoot(root *os.Root, entryInfo os.FileInfo, entryErr error) error {
	rootInfo, rootErr := root.Stat(".")
	if entryErr != nil || rootErr != nil {
		return errors.Join(entryErr, rootErr)
	}
	if !entryInfo.IsDir() || entryInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(entryInfo, rootInfo) {
		return errors.New("root directory changed while opening")
	}
	return nil
}

func (s *Store) FindSessionFile(ctx context.Context, id session.SessionFileID) (session.SessionFile, error) {
	if strings.HasPrefix(string(id), artifactIDPrefix) {
		return s.FindArtifact(ctx, id)
	}
	root := filepath.Join(s.attachmentsRoot(), "sessions")
	var found session.SessionFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".partial") {
			return nil
		}
		input, ok := s.sessionInputFromPath(root, path)
		if !ok || input.ID != id {
			return nil
		}
		found = input
		return errArtifactFound
	})
	if errors.Is(err, errArtifactFound) {
		return found, nil
	}
	if err != nil {
		return session.SessionFile{}, &Error{Code: classify(err), Path: root, Err: err}
	}
	return session.SessionFile{}, session.ErrSessionFileNotFound
}

func (s *Store) ListSessionAttachments(ctx context.Context, sessionID session.ID) ([]session.SessionAttachment, error) {
	root := filepath.Join(s.attachmentsRoot(), "sessions", safeSessionPathComponent(sessionID))
	attachments := make([]session.SessionAttachment, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".partial") {
			return nil
		}
		attachment, ok := s.sessionInputFromPath(root, path)
		if ok {
			attachments = append(attachments, attachment)
		}
		return nil
	})
	if err != nil {
		return nil, &Error{Code: classify(err), Path: root, Err: err}
	}
	sort.Slice(attachments, func(i, j int) bool {
		if attachments[i].CreatedAt.Equal(attachments[j].CreatedAt) {
			return attachments[i].ID < attachments[j].ID
		}
		return attachments[i].CreatedAt.Before(attachments[j].CreatedAt)
	})
	return attachments, nil
}

func (s *Store) ListPromptAppendAttachments(ctx context.Context, sessionID session.ID, appendID string) ([]session.SessionAttachment, error) {
	attachments, err := s.ListSessionAttachments(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	result := make([]session.SessionAttachment, 0)
	for _, attachment := range attachments {
		if attachment.SourceType == session.AttachmentSourcePromptAppend && attachment.SourceID == appendID {
			result = append(result, attachment)
		}
	}
	return result, nil
}

func (s *Store) artifactFromFile(root *os.Root, sessionID session.ID, logicalPath string) (session.SessionFile, error) {
	path := filepath.Join(s.ArtifactDir(sessionID), filepath.FromSlash(logicalPath))
	file, info, err := openArtifactFile(root, logicalPath, path)
	if err != nil {
		return session.SessionFile{}, err
	}
	defer file.Close()
	return s.artifactFromOpenFile(sessionID, logicalPath, path, file, info), nil
}

func (s *Store) artifactFromOpenFile(sessionID session.ID, logicalPath string, path string, file *os.File, info os.FileInfo) session.SessionFile {
	mimeType := detectMimeType(file, info.Name())
	artifactKind, kind := classifyArtifact(mimeType)
	if kind == session.PreviewKindImage {
		if _, err := file.Seek(0, 0); err != nil || validateImageDimensions(file, mimeType, path) != nil {
			kind = session.PreviewKindNone
		}
	}
	modifiedAt := info.ModTime().UTC()
	sourceType := session.AttachmentSourceCodex
	parts := strings.Split(logicalPath, "/")
	if len(parts) > 2 && parts[0] == "browser" {
		sourceType = session.AttachmentSourcePlaywright
	}
	return session.SessionFile{
		ID:           encodeArtifactID(sessionID, logicalPath),
		SessionID:    sessionID,
		Role:         session.FileRoleArtifact,
		SourceType:   sourceType,
		Kind:         "file",
		ArtifactKind: artifactKind,
		LogicalPath:  logicalPath,
		Filename:     info.Name(),
		Path:         path,
		MimeType:     mimeType,
		Size:         info.Size(),
		Previewable:  kind != session.PreviewKindNone,
		PreviewKind:  kind,
		CreatedAt:    modifiedAt,
	}
}

func openArtifactFile(root *os.Root, logicalPath string, path string) (*os.File, os.FileInfo, error) {
	if logicalPath == "." || !fs.ValidPath(logicalPath) {
		return nil, nil, &Error{Code: "invalid_logical_path", Path: path}
	}
	file, err := root.OpenFile(logicalPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, &Error{Code: classify(err), Path: path, Err: err}
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, &Error{Code: classify(err), Path: path, Err: err}
	}
	pathInfo, err := validateArtifactPath(root, logicalPath, path)
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(info, pathInfo) {
		_ = file.Close()
		return nil, nil, &Error{Code: "not_regular_file", Path: path}
	}
	return file, info, nil
}

func validateArtifactPath(root *os.Root, logicalPath string, path string) (os.FileInfo, error) {
	current := ""
	parts := strings.Split(logicalPath, "/")
	var info os.FileInfo
	for index, component := range parts {
		current = pathpkg.Join(current, component)
		var err error
		info, err = root.Lstat(current)
		if err != nil {
			return nil, &Error{Code: classify(err), Path: path, Err: err}
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, &Error{Code: "symlink_rejected", Path: path}
		}
		if index < len(parts)-1 && !info.IsDir() {
			return nil, &Error{Code: "not_regular_file", Path: path}
		}
	}
	if info == nil || !info.Mode().IsRegular() {
		return nil, &Error{Code: "not_regular_file", Path: path}
	}
	return info, nil
}

func findArtifactPath(ctx context.Context, root *os.Root, digest string) (string, error) {
	var found string
	err := walkArtifactRoot(ctx, root, func(logicalPath string, _ os.FileInfo) error {
		if artifactPathDigest(logicalPath) != digest {
			return nil
		}
		found = logicalPath
		return errArtifactFound
	})
	if errors.Is(err, errArtifactFound) {
		return found, nil
	}
	if err != nil {
		return "", &Error{Code: classify(err), Path: root.Name(), Err: err}
	}
	return "", session.ErrSessionFileNotFound
}

func (s *Store) walkArtifacts(ctx context.Context, sessionID session.ID, visit func(root *os.Root, logicalPath string, info os.FileInfo) error) error {
	root, err := s.openArtifactRoot(ctx, sessionID)
	if errors.Is(err, session.ErrSessionFileNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	err = walkArtifactRoot(ctx, root, func(logicalPath string, info os.FileInfo) error {
		return visit(root, logicalPath, info)
	})
	if err != nil {
		var storeErr *Error
		if errors.As(err, &storeErr) {
			return err
		}
		return &Error{Code: classify(err), Path: s.ArtifactDir(sessionID), Err: err}
	}
	return nil
}

func walkArtifactRoot(ctx context.Context, root *os.Root, visit func(logicalPath string, info os.FileInfo) error) error {
	return fs.WalkDir(root.FS(), ".", func(logicalPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if logicalPath == "." || entry.IsDir() || strings.HasSuffix(entry.Name(), ".partial") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return visit(logicalPath, info)
	})
}

func (s *Store) artifactDirectoryState(ctx context.Context, sessionID session.ID) (string, error) {
	var state strings.Builder
	err := s.walkArtifacts(ctx, sessionID, func(_ *os.Root, logicalPath string, _ os.FileInfo) error {
		state.WriteString(logicalPath)
		state.WriteByte(0)
		return nil
	})
	if err != nil {
		return "", err
	}
	return state.String(), nil
}

func (s *Store) sessionInputFromPath(root string, path string) (session.SessionFile, bool) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return session.SessionFile{}, false
	}
	parts := strings.Split(filepath.ToSlash(relative), "/")
	var sessionID session.ID
	var sourceType session.AttachmentSourceType
	var sourceID string
	var id session.SessionFileID
	if filepath.Base(root) == "sessions" {
		if len(parts) == 5 {
			sessionID = session.ID(parts[0])
			sourceType = session.AttachmentSourceType(parts[1])
			sourceIDBytes, decodeErr := base64.RawURLEncoding.DecodeString(parts[2])
			if decodeErr != nil {
				return session.SessionFile{}, false
			}
			sourceID = string(sourceIDBytes)
			id = session.SessionFileID(parts[3])
		} else if len(parts) == 3 {
			sessionID = session.ID(parts[0])
			sourceType = session.AttachmentSourceRequirement
			sourceID = string(sessionID)
			id = session.SessionFileID(parts[1])
		} else {
			return session.SessionFile{}, false
		}
	} else {
		sessionID = session.ID(filepath.Base(root))
		if len(parts) == 4 {
			sourceType = session.AttachmentSourceType(parts[0])
			sourceIDBytes, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
			if decodeErr != nil {
				return session.SessionFile{}, false
			}
			sourceID = string(sourceIDBytes)
			id = session.SessionFileID(parts[2])
		} else if len(parts) == 2 {
			sourceType = session.AttachmentSourceRequirement
			sourceID = string(sessionID)
			id = session.SessionFileID(parts[0])
		} else {
			return session.SessionFile{}, false
		}
	}
	if id == "" || (sourceType != session.AttachmentSourceRequirement && sourceType != session.AttachmentSourcePromptAppend) {
		return session.SessionFile{}, false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return session.SessionFile{}, false
	}
	mimeType := detectAttachmentMimeType(path, info.Name())
	_, kind := classifyArtifact(mimeType)
	return session.SessionFile{
		ID:          id,
		SessionID:   sessionID,
		Role:        session.FileRoleInput,
		SourceType:  sourceType,
		SourceID:    sourceID,
		Kind:        "file",
		Filename:    info.Name(),
		Path:        path,
		MimeType:    mimeType,
		Size:        info.Size(),
		Previewable: kind != session.PreviewKindNone,
		PreviewKind: kind,
		CreatedAt:   info.ModTime().UTC(),
	}, true
}

func (s *Store) sessionInputDir(sessionID session.ID, sourceType session.AttachmentSourceType, sourceID string, id session.SessionFileID) string {
	encodedSourceID := base64.RawURLEncoding.EncodeToString([]byte(sourceID))
	return filepath.Join(s.attachmentsRoot(), "sessions", safeSessionPathComponent(sessionID), string(sourceType), encodedSourceID, string(id))
}

func encodeArtifactID(sessionID session.ID, logicalPath string) session.SessionFileID {
	encodedSession := base64.RawURLEncoding.EncodeToString([]byte(sessionID))
	return session.SessionFileID(artifactIDPrefix + encodedSession + "." + artifactPathDigest(logicalPath))
}

func decodeArtifactID(id session.SessionFileID) (session.ID, string, bool) {
	value := strings.TrimPrefix(string(id), artifactIDPrefix)
	if value == string(id) {
		return "", "", false
	}
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 || len(parts[1]) != sha256.Size*2 {
		return "", "", false
	}
	sessionID, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || cleanPathComponent(string(sessionID)) != string(sessionID) {
		return "", "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", "", false
	}
	return session.ID(sessionID), parts[1], true
}

func artifactPathDigest(logicalPath string) string {
	hash := sha256.Sum256([]byte(logicalPath))
	return hex.EncodeToString(hash[:])
}

func normalizeArtifactPath(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	if value == "" || strings.HasPrefix(value, "/") || (len(value) >= 2 && value[1] == ':') {
		return "", &Error{Code: "invalid_logical_path", Path: value}
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." {
			return "", &Error{Code: "invalid_logical_path", Path: value}
		}
	}
	normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", &Error{Code: "invalid_logical_path", Path: value}
	}
	return normalized, nil
}

func previewKind(mimeType string) session.PreviewKind {
	_, kind := classifyArtifact(mimeType)
	return kind
}

func isFilestoreNotFound(err error) bool {
	var storeErr *Error
	return errors.As(err, &storeErr) && errors.Is(storeErr.Err, os.ErrNotExist)
}

var errArtifactFound = errors.New("artifact found")
