using System;
using System.Collections.Generic;
using System.Linq;

namespace App.Models
{
    // Interface with properties
    public interface IRepository<T>
    {
        T GetById(int id);
        void Save(T entity);
    }

    // Abstract class
    public abstract class BaseEntity
    {
        public int Id { get; set; }

        public abstract string Validate();

        public virtual string Display()
        {
            return $"Entity {Id}";
        }
    }

    // Class inheriting from abstract
    public class User : BaseEntity
    {
        private string _name;

        public User(string name)
        {
            _name = name;
        }

        public override string Validate()
        {
            return string.IsNullOrEmpty(_name) ? "Invalid" : "Valid";
        }

        public string GetName()
        {
            return _name;
        }
    }

    // Static class
    public static class Helper
    {
        public static string Format(string input)
        {
            return input.Trim().ToLower();
        }
    }

    // Enum
    public enum Status
    {
        Active,
        Inactive,
        Deleted
    }

    // Struct
    public struct Point
    {
        public double X;
        public double Y;

        public Point(double x, double y)
        {
            X = x;
            Y = y;
        }

        public double Distance()
        {
            return Math.Sqrt(X * X + Y * Y);
        }
    }
}
