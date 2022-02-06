package routes

import (
	"os"
	"strconv"
	"time"

	"github.com/ShivanshVerma-coder/url-shortening-service/database"
	"github.com/ShivanshVerma-coder/url-shortening-service/helpers"
	"github.com/asaskevich/govalidator"
	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type request struct {
	URL         string        `json:"url"`
	CustomShort string        `json:"custom_short"`
	Expiry      time.Duration `json:"expiry"`
}

type response struct {
	URL             string        `json:"url"`
	CustomShort     string        `json:"custom_short"`
	Expiry          time.Duration `json:"expiry"`
	XRateRemaining  int           `json:"rate_limit"`
	XRateLimitReset time.Duration `json:"rate_limit_reset"`
}

func ShortenURL(c *fiber.Ctx) error {
	body := new(request)

	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Bad request"})
	}

	//implement rate limiting
	r2 := database.CreateClient(1)
	defer r2.Close()

	var XRateRemaining int
	var XRateLimitReset time.Duration
	value, err := r2.Get(database.Ctx, c.IP()).Result()

	if err == redis.Nil {
		_ = r2.Set(database.Ctx, c.IP(), os.Getenv("API_QUOTA"), time.Second*30*60).Err()
		XRateRemaining, _ = strconv.Atoi(os.Getenv("API_QUOTA"))
		XRateLimitReset = time.Second * 30 * 60
	} else {
		XRateRemaining, _ = strconv.Atoi(value)
		XRateLimitReset, _ := r2.TTL(database.Ctx, c.IP()).Result()
		if XRateRemaining <= 0 {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "Rate limit exceeded", "rate_limit_reset": XRateLimitReset / time.Nanosecond / time.Minute})
		}
	}

	//check if the input if an actual URL

	if !govalidator.IsURL(body.URL) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid URL"})
	}

	// check for domain error

	if !helpers.RemoveDomainError(body.URL) {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "You cant hack the system :)"})
	}

	// enfore http, SSL
	body.URL = helpers.EnforceHTTP(body.URL)

	var id string

	if body.CustomShort == "" {
		id = uuid.New().String()[:6]
	} else {
		id = body.CustomShort
	}

	r := database.CreateClient(0)
	defer r.Close()

	val, _ := r.Get(database.Ctx, id).Result()
	if val != "" {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Custom short URL already exists"})
	}

	err = r.Set(database.Ctx, id, body.URL, body.Expiry*3600*time.Second).Err()

	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Unable to connect to server",
		})
	}

	r2.Decr(database.Ctx, c.IP())

	var CustomShort string = os.Getenv("DOMAIN") + "/" + id

	resp := response{
		URL:             body.URL,
		CustomShort:     CustomShort,
		Expiry:          body.Expiry,
		XRateRemaining:  XRateRemaining - 1,
		XRateLimitReset: XRateLimitReset / time.Nanosecond / time.Minute,
	}

	return c.Status(fiber.StatusCreated).JSON(resp)

}
