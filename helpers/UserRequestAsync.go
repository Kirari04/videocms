package helpers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"time"
)

type UserRequestAsync struct {
	Count uint
	Map   map[uint]bool
	Block bool
}

var UserRequestAsyncObj UserRequestAsync

func (obj *UserRequestAsync) Blocked(userID uint) bool {
	if obj.Map[userID] || obj.Block {
		return true
	}

	return false
}

func (obj *UserRequestAsync) Sync(force bool) error {
	// query all users
	var userCount int64
	res := inits.DB.Model(&models.User{}).
		Select("id").
		Count(&userCount)

	if res.Error != nil {
		return fmt.Errorf("failed to query all users to sync Request Map: %v", res.Error)
	}
	obj.Block = true
	if len(obj.Map) == 0 || force {
		obj.Map = make(map[uint]bool, userCount)
		obj.Block = false
	} else {
		// await until all requests are closed
		for obj.Block {
			time.Sleep(time.Millisecond * 200)
			if obj.Count == 0 {
				obj.Map = make(map[uint]bool, userCount)
				obj.Block = false
			}
		}
	}
	return nil
}

func (obj *UserRequestAsync) Start(userId uint) {
	obj.Map[userId] = true
}

func (obj *UserRequestAsync) End(userId uint) {
	obj.Map[userId] = false
}
