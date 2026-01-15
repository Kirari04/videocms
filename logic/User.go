package logic

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"errors"
	"net/http"
)

func GetUsers() (int, *[]models.User, error) {
	users := make([]models.User, 0)
	if res := inits.DB.Find(&users); res.Error != nil {
		return http.StatusInternalServerError, nil, errors.New("failed to fetch users")
	}
	return http.StatusOK, &users, nil
}

func CreateUser(username, password, email string, admin bool, storage int64, balance float64) (int, *models.User, error) {
	hash, err := helpers.HashPassword(password)
	if err != nil {
		return http.StatusInternalServerError, nil, errors.New("failed to process password")
	}

	user := models.User{
		Username: username,
		Hash:     hash,
		Email:    email,
		Admin:    admin,
		Storage:  storage,
		Balance:  balance,
	}

	if result := inits.DB.Create(&user); result.Error != nil {
		return http.StatusInternalServerError, nil, errors.New("failed to create user")
	}

	return http.StatusCreated, &user, nil
}

func GetUser(id uint64) (int, *models.User, error) {
	var user models.User
	if result := inits.DB.First(&user, id); result.Error != nil {
		return http.StatusNotFound, nil, errors.New("user not found")
	}
	return http.StatusOK, &user, nil
}

func UpdateUser(id uint64, username, email string, admin *bool, storage *int64, balance *float64) (int, *models.User, error) {
	var user models.User
	if result := inits.DB.First(&user, id); result.Error != nil {
		return http.StatusNotFound, nil, errors.New("user not found")
	}

	if username != "" {
		user.Username = username
	}
	if email != "" {
		user.Email = email
	}
	if admin != nil {
		user.Admin = *admin
	}
	if storage != nil {
		user.Storage = *storage
	}
	if balance != nil {
		user.Balance = *balance
	}

	if result := inits.DB.Save(&user); result.Error != nil {
		return http.StatusInternalServerError, nil, errors.New("failed to update user")
	}

	return http.StatusOK, &user, nil
}

func DeleteUser(id uint64) (int, error) {
	if result := inits.DB.Delete(&models.User{}, id); result.Error != nil {
		return http.StatusInternalServerError, errors.New("failed to delete user")
	}
	return http.StatusNoContent, nil
}

func ResetUserPassword(id uint64, newPassword string) (int, error) {
	var user models.User
	if result := inits.DB.First(&user, id); result.Error != nil {
		return http.StatusNotFound, errors.New("user not found")
	}

	hash, err := helpers.HashPassword(newPassword)
	if err != nil {
		return http.StatusInternalServerError, errors.New("failed to process password")
	}

	user.Hash = hash
	if result := inits.DB.Save(&user); result.Error != nil {
		return http.StatusInternalServerError, errors.New("failed to update password")
	}

	return http.StatusOK, nil
}
