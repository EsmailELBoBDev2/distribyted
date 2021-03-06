package fuse

import (
	"fmt"
	"os"
	"path"
	"runtime"

	"github.com/ajnavarro/distribyted/config"
	"github.com/ajnavarro/distribyted/fs"
	"github.com/ajnavarro/distribyted/stats"
	"github.com/anacrolix/torrent"
	"github.com/billziss-gh/cgofuse/fuse"
	log "github.com/sirupsen/logrus"
)

type Handler struct {
	c *torrent.Client
	s *stats.Torrent

	hosts map[string]*fuse.FileSystemHost
}

func NewHandler(c *torrent.Client, s *stats.Torrent) *Handler {
	return &Handler{
		c:     c,
		s:     s,
		hosts: make(map[string]*fuse.FileSystemHost),
	}
}

func (s *Handler) Mount(mpc *config.MountPoint, ef config.EventFunc) error {
	var torrents []fs.Filesystem
	for _, mpcTorrent := range mpc.Torrents {
		var t *torrent.Torrent
		var err error

		switch {
		case mpcTorrent.MagnetURI != "":
			t, err = s.c.AddMagnet(mpcTorrent.MagnetURI)
			break
		case mpcTorrent.TorrentPath != "":
			t, err = s.c.AddTorrentFromFile(mpcTorrent.TorrentPath)
			break
		default:
			err = fmt.Errorf("no magnet URI or torrent path provided")
		}
		if err != nil {
			return err
		}

		// only get info if name is not available
		if t.Name() == "" {
			ef(fmt.Sprintf("getting torrent info...: %v", t.InfoHash()))
			log.WithField("hash", t.InfoHash()).Info("getting torrent info")
			<-t.GotInfo()
		}

		s.s.Add(mpc.Path, t)
		torrents = append(torrents, fs.NewTorrent(t))

		ef(fmt.Sprintf("torrent %v added to mountpoint", t.Name()))
		log.WithField("name", t.Name()).WithField("path", mpc.Path).Info("torrent added to mountpoint")
	}

	folder := mpc.Path
	// On windows, the folder must don't exist
	if runtime.GOOS == "windows" {
		folder = path.Dir(folder)
	}
	if err := os.MkdirAll(folder, 0744); err != nil && !os.IsExist(err) {
		return err
	}

	host := fuse.NewFileSystemHost(NewFS(torrents))

	// TODO improve error handling here
	go func() {
		ok := host.Mount(mpc.Path, nil)
		if !ok {
			log.WithField("path", mpc.Path).Error("error trying to mount filesystem")
		}
	}()

	s.hosts[mpc.Path] = host

	return nil
}

func (s *Handler) UnmountAll() {
	for path, server := range s.hosts {
		log.WithField("path", path).Info("unmounting")
		ok := server.Unmount()
		if !ok {
			//TODO try to force unmount if possible
			log.WithField("path", path).Error("unmount failed")
		}
	}
	s.hosts = make(map[string]*fuse.FileSystemHost)
	s.s.RemoveAll()
}
